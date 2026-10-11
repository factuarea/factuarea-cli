"""Native CLI orchestration: no HTTP engine, credential journal or resend fallback."""

import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import subprocess
import sys
from urllib.parse import urlsplit


NATIVE_HASH = "438b12b53be7ae7535cd6c85ba7dea06c76d84a33024db0e0232ac8ba279be63"
UUID = re.compile(r"^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-7[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$")
SANDBOX_KEY = re.compile(r"^fact_test_[A-Za-z0-9]{24}$")
IDS = {
    "lead": ("crmCreateLead", "crmFindLead", "crmGetLeadCreateOriginal"),
    "opportunity": ("crmCreateOpportunity", "crmGetOpportunity", "crmGetOpportunityCreateOriginal"),
    "activity": ("crmCreateCrmActivity", "crmGetCrmActivity", "crmGetCrmActivityCreateOriginal"),
}


class JourneyBoundaryError(Exception):
    """Boundary names only; never supplied values or API error bodies."""


def safe_base_url(value):
    parsed = urlsplit(value)
    if parsed.scheme not in ("http", "https") or not parsed.hostname:
        raise JourneyBoundaryError("La URL base exige host HTTP(S)")
    if parsed.username is not None or parsed.password is not None or parsed.query or parsed.fragment:
        raise JourneyBoundaryError("La URL base no admite credenciales/query/fragmento")
    if parsed.path not in ("", "/"):
        raise JourneyBoundaryError("La URL base no incorpora rutas de recursos")
    if parsed.scheme == "http":
        try:
            loopback = ipaddress.ip_address(parsed.hostname).is_loopback
        except ValueError:
            loopback = parsed.hostname.lower() == "localhost"
        if not loopback:
            raise JourneyBoundaryError("HTTP sólo admite fixture loopback de Root")
    try:
        parsed.port
    except ValueError:
        raise JourneyBoundaryError("La URL base declara un puerto inválido") from None
    return value.rstrip("/")


def native_plan(input_path, ids=None):
    metadata = json.loads(Path(input_path).read_bytes())
    raw = Path(metadata["artifact"]).read_bytes()
    native_hash = hashlib.sha256(raw).hexdigest()
    if native_hash != metadata["artifact_sha256"] or native_hash != NATIVE_HASH:
        raise JourneyBoundaryError("El artefacto no coincide con Source embebido")
    native = json.loads(raw)
    profile = native["x-factuarea-contract-profile"]
    if profile["state"] != "unreleased" or any(
        metadata[name] is not False or profile[name] is not False
        for name in ("ratified", "published", "delivery_enabled")
    ) or metadata["capabilities"] != []:
        raise JourneyBoundaryError("La receta conserva gate Source privado y CRM OFF")
    plan = []
    for entity, operations in IDS.items():
        step = {"entity": entity, "identity": (ids or {}).get(entity), "operations": {}}
        for action, method, operation_id in zip(("create", "current", "original"), ("post", "get", "get"), operations):
            matches = [
                (path, item[method]) for path, item in native["paths"].items()
                if method in item and item[method].get("operationId") == operation_id
            ]
            if len(matches) != 1:
                raise JourneyBoundaryError("La operación nativa no está declarada una vez")
            path, operation = matches[0]
            if not operation.get("x-required-scope") or (action == "create" and "requestBody" not in operation):
                raise JourneyBoundaryError("Falta scope/requestBody nativo")
            step["operations"][action] = {
                "method": method.upper(), "route_template": "/v1" + path,
                "scope": operation["x-required-scope"], "operation_id": operation_id,
            }
        plan.append(step)
    return native_hash, plan


def redacted_plan(native_hash, plan):
    return {
        "state": "native_source_journey_plan", "mode": "dry-run",
        "native_sha256": native_hash, "network_calls": 0, "writes": 0,
        "current_is_original": False,
        "steps": [{"entity": step["entity"], "operations": step["operations"]} for step in plan],
        "cas": "activity update reads Current before PATCH; server409 stops without retry",
        "recovery": "create Original GET retains raw/key; PATCH has no public Original route",
    }


def sandbox_keys(environment):
    keys = {}
    for realm in ("A", "B"):
        value = environment.get("FACTUAREA_JOURNEY_" + realm + "_KEY", "")
        if SANDBOX_KEY.fullmatch(value) is None:
            raise JourneyBoundaryError("A/B requieren credenciales sandbox nativas por entorno")
        keys[realm] = value
    if keys["A"] == keys["B"]:
        raise JourneyBoundaryError("A/B no reutilizan credencial")
    return keys


def literal_intent_key(value):
    if not isinstance(value, str) or not 1 <= len(value) <= 255 or value.strip() != value:
        raise JourneyBoundaryError("Conserva clave original explícita representable en HTTP")
    if any(ord(char) < 32 or ord(char) > 126 for char in value):
        raise JourneyBoundaryError("La clave nativa exige ASCII imprimible")
    return value


def invoke_cli(binary, base, credential, argv, environment, raw=None, intent_key=None):
    child = dict(environment)
    for name in list(child):
        if name.startswith("FACTUAREA_JOURNEY_"):
            child.pop(name)
    child["FACTUAREA_API_KEY"] = credential
    child["FACTUAREA_BASE_URL"] = safe_base_url(base)
    child.pop("FACTUAREA_PROFILE", None)
    child.pop("FACTUAREA_CRM_JOURNEY_INTENT", None)
    if intent_key is not None:
        child["FACTUAREA_CRM_JOURNEY_INTENT"] = literal_intent_key(intent_key)
    streams = {"input": raw} if raw is not None else {"stdin": subprocess.DEVNULL}
    try:
        result = subprocess.run(
            [binary, "--json", "--no-input", *argv], env=child,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=65, check=False, **streams,
        )
    except (OSError, subprocess.TimeoutExpired):
        raise JourneyBoundaryError("Resultado UNKNOWN; no se reenvió") from None
    if result.returncode != 0:
        return result.returncode, None
    try:
        return 0, json.loads(result.stdout)
    except (ValueError, UnicodeDecodeError):
        raise JourneyBoundaryError("Respuesta CLI sin envelope nativo") from None


def realms(binary, base, environment):
    keys = sandbox_keys(environment)
    companies = {}
    for realm, key in keys.items():
        code, account = invoke_cli(binary, base, key, ["api", "get", "/v1/account"], environment)
        data = account.get("data") if code == 0 and isinstance(account, dict) else None
        company_data = data.get("company") if isinstance(data, dict) else None
        key_data = data.get("api_key") if isinstance(data, dict) else None
        company = company_data.get("id") if isinstance(company_data, dict) else None
        if not isinstance(company, str) or not company:
            raise JourneyBoundaryError("Account no acredita Company actual")
        scopes = key_data.get("scopes", []) if isinstance(key_data, dict) else []
        if not isinstance(scopes, list) or any(
            scope not in scopes and "*" not in scopes
            for scope in ("crm_leads:read", "crm_opportunities:read", "crm_activities:read")
        ):
            raise JourneyBoundaryError("La fixture nativa carece de scopes de lectura del journey")
        companies[realm] = company
    if companies["A"] == companies["B"]:
        raise JourneyBoundaryError("A/B no acreditan Company distintas")
    return keys


def current_record(binary, base, keys, entity, identity, environment):
    if not isinstance(identity, str) or UUID.fullmatch(identity) is None:
        raise JourneyBoundaryError("Current exige UUIDv7 nativo explícito")
    args = ["crm", entity, "current", identity, "--source-contracts"]
    code, result = invoke_cli(binary, base, keys["A"], args, environment)
    data = result.get("data") if code == 0 and isinstance(result, dict) else None
    record = data.get("activity") if entity == "activity" and isinstance(data, dict) else data
    if not isinstance(record, dict) or record.get("id") != identity:
        raise JourneyBoundaryError("Current no acredita el recurso visible de A")
    version = record.get("version")
    if not isinstance(version, int) or isinstance(version, bool) or version < 1:
        raise JourneyBoundaryError("La revisión Current no permite CAS")
    foreign, _ = invoke_cli(binary, base, keys["B"], args, environment)
    if foreign not in (4, 6):
        raise JourneyBoundaryError("B no deniega actualmente el recurso de A")
    return record


def read_sandboxes(binary, base, plan, environment=None):
    environment = os.environ if environment is None else environment
    keys = realms(binary, base, environment)
    evidence = []
    for step in plan:
        current = current_record(binary, base, keys, step["entity"], step["identity"], environment)
        evidence.append({"entity": step["entity"], "current_version": current["version"], "foreign_denied": True})
    return {"state": "native_current_reads", "writes": 0, "current_is_original": False, "steps": evidence}


def original(binary, base, keys, entity, raw, key, environment):
    code, payload = invoke_cli(binary, base, keys["A"], [
        "crm", entity, "original", "--source-contracts", "--data", "-",
        "--idempotency-key-env", "FACTUAREA_CRM_JOURNEY_INTENT",
    ], environment, raw=raw, intent_key=key)
    data = payload.get("data") if code == 0 and isinstance(payload, dict) else None
    if not isinstance(data, dict) or data.get("original_body_sha256") != hashlib.sha256(raw).hexdigest():
        raise JourneyBoundaryError("Original no conserva intención bajo autoridad actual")
    return data


def recover_original(binary, base, entity, body_file, environment=None):
    environment = os.environ if environment is None else environment
    keys = sandbox_keys(environment)
    if not body_file:
        raise JourneyBoundaryError("Original exige fichero exacto del primer POST")
    raw = Path(body_file).read_bytes()
    key = literal_intent_key(environment.get("FACTUAREA_JOURNEY_" + entity.upper() + "_INTENT", ""))
    result = original(binary, base, keys, entity, raw, key, environment)
    return {"state": result["state"], "method": "GET", "writes": 0, "current_is_original": False}


def created_id(entity, envelope):
    data = envelope.get("data") if isinstance(envelope, dict) else None
    if not isinstance(data, dict):
        raise JourneyBoundaryError("Create no devolvió receipt nativo")
    identity = data.get("activity_id" if entity == "activity" else "id")
    if not isinstance(identity, str) or UUID.fullmatch(identity) is None:
        raise JourneyBoundaryError("Create no acredita UUIDv7 reservado por el dueño")
    return identity


def run_journey(binary, base, plan, environment=None, prompt=input):
    environment = os.environ if environment is None else environment
    keys = realms(binary, base, environment)
    identities, evidence, intent_keys = {}, [], set()
    for step in plan:
        entity = step["entity"]
        if identities:
            predecessor = "lead" if entity == "opportunity" else "opportunity"
            print("Referencia nativa para el siguiente fichero: " + identities[predecessor], file=sys.stderr)
            current_record(binary, base, keys, predecessor, identities[predecessor], environment)
        raw = Path(prompt("Fichero privado revisado para " + entity + " create: ")).read_bytes()
        body = json.loads(raw)
        if not isinstance(body, dict):
            raise JourneyBoundaryError("El fichero revisado exige un objeto JSON nativo")
        if entity == "opportunity" and body.get("lead_id") != identities["lead"]:
            raise JourneyBoundaryError("Opportunity debe enlazar el Lead nativo recién leído")
        if entity == "activity" and body.get("target") != {"type": "opportunity", "id": identities["opportunity"]}:
            raise JourneyBoundaryError("Activity debe apuntar al Opportunity nativo recién leído")
        key = literal_intent_key(environment.get("FACTUAREA_JOURNEY_" + entity.upper() + "_INTENT", ""))
        if key in intent_keys:
            raise JourneyBoundaryError("Cada efecto requiere clave explícita distinta")
        intent_keys.add(key)
        if prompt("Confirma escribiendo CREATE " + entity + ": ") != "CREATE " + entity:
            raise JourneyBoundaryError("Confirmación humana cancelada")
        code, receipt = invoke_cli(binary, base, keys["A"], [
            "crm", entity, "create", "--source-contracts", "--confirmed", "--data", "-",
            "--idempotency-key-env", "FACTUAREA_CRM_JOURNEY_INTENT",
        ], environment, raw=raw, intent_key=key)
        if code != 0:
            raise JourneyBoundaryError("Create no terminó confirmado; usa sólo GET Original")
        identity = created_id(entity, receipt)
        proof = original(binary, base, keys, entity, raw, key, environment)
        response = proof.get("original_response")
        if proof.get("state") != "confirmed" or not isinstance(response, dict) or created_id(entity, json.loads(response["body"])) != identity:
            raise JourneyBoundaryError("Original no confirmó el mismo target; el siguiente paso no empieza")
        record = current_record(binary, base, keys, entity, identity, environment)
        if entity == "opportunity" and record.get("lead_id") != identities["lead"]:
            raise JourneyBoundaryError("La asociación actual Opportunity/Lead no se acreditó")
        if entity == "activity" and {"type": "opportunity", "id": identities["opportunity"]} not in record.get("targets", []):
            raise JourneyBoundaryError("La asociación actual Activity/Opportunity no se acreditó")
        identities[entity] = identity
        evidence.append({"entity": entity, "original_state": "confirmed", "current_version": record["version"], "foreign_denied": True})
    print("Activity actual para PATCH: " + identities["activity"] + "; version=" + str(record["version"]), file=sys.stderr)
    raw = Path(prompt("Fichero privado PATCH con expected_version actual: ")).read_bytes()
    body = json.loads(raw)
    if not isinstance(body, dict):
        raise JourneyBoundaryError("PATCH exige objeto JSON nativo")
    if type(body.get("expected_version")) is not int or body["expected_version"] != record["version"]:
        raise JourneyBoundaryError("CAS exige versión Current publicada")
    key = literal_intent_key(environment.get("FACTUAREA_JOURNEY_UPDATE_INTENT", ""))
    if key in intent_keys or prompt("Confirma escribiendo UPDATE activity: ") != "UPDATE activity":
        raise JourneyBoundaryError("Update exige nueva intención y confirmación humana")
    code, result = invoke_cli(binary, base, keys["A"], [
        "crm", "activity", "update", identities["activity"], "--source-contracts", "--confirmed", "--data", "-",
        "--idempotency-key-env", "FACTUAREA_CRM_JOURNEY_INTENT",
    ], environment, raw=raw, intent_key=key)
    if code == 8:
        fresh = current_record(binary, base, keys, "activity", identities["activity"], environment)
        return {"state": "native_cas_conflict", "current_version": fresh["version"], "post_retry": False,
                "current_is_original": False, "steps": evidence}
    if code != 0 or created_id("activity", result) != identities["activity"]:
        raise JourneyBoundaryError("PATCH no se confirmó; no hay Original público y no se reenvía")
    fresh = current_record(binary, base, keys, "activity", identities["activity"], environment)
    return {"state": "native_journey_confirmed", "post_retry": False, "current_is_original": False,
            "activity_current_version": fresh["version"], "steps": evidence, "update_original_available": False}

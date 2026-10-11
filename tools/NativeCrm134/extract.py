#!/usr/bin/env python3
"""Generate the private Stage/Bulk/Restore Original consumer from one real native export.

The shared extractor owns the native reference closure. This adapter does not run
the application, alter the public spec, install packages or publish the contract.
"""

import argparse
import hashlib
import importlib.util
import json
from pathlib import Path


SELECTED = {
    "stage_list": ("/crm/service-stages", "get"),
    "stage_update": ("/crm/service-stages/{stage}", "patch"),
    "stage_original": ("/crm/service-stages/{stage}/original", "get"),
    "bulk_options": ("/crm/service-tickets/bulk-actions/tag-options", "get"),
    "bulk_apply": ("/crm/service-tickets/bulk-actions", "post"),
    "bulk_original": ("/crm/service-tickets/{ticket}/bulk-actions/original", "get"),
    "restore_original": ("/crm/documents/{document}/restore/original", "get"),
}

EXPECTED = {
    "stage_list": ("listServiceStages", "customer_service:read"),
    "stage_update": ("updateServiceStage", "customer_service:write"),
    "stage_original": ("getServiceStageOriginal", "customer_service:write"),
    "bulk_options": ("getServiceTicketBulkOptions", "customer_service:read"),
    "bulk_apply": ("applyServiceTicketBulkAction", "customer_service:write"),
    "bulk_original": ("getOriginalServiceTicketBulkItem", "customer_service:write"),
    "restore_original": ("crmGetCommercialDocumentRestoreOriginal", "crm_documents:read"),
}

BOUNDARIES = {
    "restore_intent": ("/crm/documents/{document}/restore", "post"),
}


def generate(input_path, output_root, native_sha256, manifest_sha256):
    metadata_bytes = input_path.read_bytes()
    actual_manifest_hash = hashlib.sha256(metadata_bytes).hexdigest()
    if len(manifest_sha256) != 64 or actual_manifest_hash != manifest_sha256:
        raise ValueError("The actual Root consumer manifest must match its explicitly delivered SHA256")
    metadata = json.loads(metadata_bytes)
    artifact = Path(metadata["artifact"])
    if not artifact.is_absolute():
        raise ValueError("The native artifact must use its durable absolute location")
    native_bytes = artifact.read_bytes()
    declared_hash = metadata.get("artifact_sha256") or metadata.get("sha256")
    actual_hash = hashlib.sha256(native_bytes).hexdigest()
    if len(native_sha256) != 64 or actual_hash != native_sha256 or declared_hash != actual_hash:
        raise ValueError("The delivered native artifact and explicit Root SHA256 must agree")
    native = json.loads(native_bytes)
    production = metadata["production"]
    if any(production[name] is not False for name in ("ratified", "published", "delivery")) or production["capabilities"] != []:
        raise ValueError("The delivered Root manifest must retain every disabled production gate")
    selected_manifest = metadata["selected_native_operations"]
    if len(selected_manifest) != len(SELECTED) or len({entry["kind"] for entry in selected_manifest}) != len(SELECTED):
        raise ValueError("The Root manifest must declare exactly the seven genuine consumer operations")
    for entry in selected_manifest:
        key = entry["kind"]
        if (entry["path"], entry["method"]) != SELECTED[key] or (entry["operation_id"], entry["required_scope"]) != EXPECTED[key]:
            raise ValueError("The actual Root operation manifest changed: " + key)
    if metadata["catalog_sha256"] != native["x-factuarea-contract-profile"]["catalog_sha256"]:
        raise ValueError("The artifact and actual Root catalog hashes must agree")
    for key, (path, method) in SELECTED.items():
        operation = native["paths"][path][method]
        operation_id, scope = EXPECTED[key]
        if operation.get("operationId") != operation_id or operation.get("x-required-scope") != scope:
            raise ValueError("Native operation or scope changed: " + key)
        if method == "get" and operation.get("requestBody") is not None:
            raise ValueError("Native Original/list readers must remain GET without request bodies")
    restore = native["paths"][BOUNDARIES["restore_intent"][0]]["post"]
    if restore.get("operationId") != "crmRestoreCommercialDocument" or restore.get("x-required-scope") != "crm_documents:write":
        raise ValueError("Restore Original must validate its genuine Restore write boundary")
    boundary = metadata["validation_only_boundary"]
    if boundary != {"method": "POST", "path": BOUNDARIES["restore_intent"][0], "operation_id": "crmRestoreCommercialDocument", "required_scope": "crm_documents:write"}:
        raise ValueError("The Root manifest must declare the same validation-only Restore POST")
    output_root = output_root.resolve()
    if tuple(output_root.parts[-4:]) != ("cli_consumers134", "old", "post", "cli"):
        raise ValueError("Generation output must be private cli_consumers134/old/post/cli")
    loader = importlib.util.spec_from_file_location(
        "native_reference_closure", Path(__file__).resolve().parents[1] / "crm-source139" / "extract.py",
    )
    shared = importlib.util.module_from_spec(loader)
    loader.loader.exec_module(shared)
    templates = Path(__file__).resolve().parent / "templates"
    targets = {
        "spec.go.tmpl": "internal/spec/crm_native134.go",
        "client.go.tmpl": "internal/client/crm_native134.go",
        "commands.go.tmpl": "internal/cmd/crm_native134.go",
    }
    template_sources = {name: (templates / name).read_text() for name in targets}
    contract_path = output_root / "internal/spec/crm_native134.openapi.json"
    # Preserve the shared extractor's legacy input ABI. Every projected gate is
    # copied from the verified actual Root production object, never synthesized.
    projection_bytes = (json.dumps({
        "artifact": str(artifact), "artifact_sha256": actual_hash,
        "ratified": production["ratified"], "published": production["published"],
        "delivery_enabled": production["delivery"], "capabilities": production["capabilities"],
        "actual_root_manifest_sha256": actual_manifest_hash,
    }, ensure_ascii=False, sort_keys=True) + "\n").encode()

    class LegacyManifestProjection:
        def read_bytes(self):
            return projection_bytes

    shared.extract(LegacyManifestProjection(), contract_path, selection=SELECTED, boundary_selection=BOUNDARIES)
    contract = json.loads(contract_path.read_bytes())
    contract["source_manifest_sha256"] = actual_manifest_hash
    contract["source_manifest_projection_sha256"] = hashlib.sha256(projection_bytes).hexdigest()
    contract_path.write_text(json.dumps(contract, ensure_ascii=False, indent=2) + "\n")
    carrier_ids = [carrier["operation_id"] for carrier in contract["carriers"]]
    expected_ids = [operation_id for operation_id, _ in EXPECTED.values()]
    if sorted(carrier_ids) != sorted(expected_ids):
        contract_path.unlink()
        raise ValueError("Every selected operation must have exactly its own native carrier")
    selected_hash = hashlib.sha256(contract_path.read_bytes()).hexdigest()
    for template, target in targets.items():
        content = template_sources[template].replace("@NATIVE_SHA256@", actual_hash).replace("@SELECTED_SHA256@", selected_hash).replace("@MANIFEST_SHA256@", actual_manifest_hash)
        destination = output_root / target
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_text(content)
    return {
        "artifact": str(artifact), "native_sha256": actual_hash,
        "manifest_sha256": actual_manifest_hash,
        "manifest_projection_sha256": hashlib.sha256(projection_bytes).hexdigest(),
        "operations": sorted(SELECTED), "selected_sha256": selected_hash,
        "boundaries": sorted(BOUNDARIES), "output_root": str(output_root),
        "published": False, "ratified": False, "delivery_enabled": False,
        "capabilities": [], "tests_executed": False,
    }


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", required=True, type=Path)
    parser.add_argument("--native-sha256", required=True)
    parser.add_argument("--manifest-sha256", required=True)
    parser.add_argument("--output-root", required=True, type=Path)
    args = parser.parse_args()
    print(json.dumps(generate(args.input, args.output_root, args.native_sha256, args.manifest_sha256), ensure_ascii=False))

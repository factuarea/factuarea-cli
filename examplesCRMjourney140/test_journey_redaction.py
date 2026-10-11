"""Prepared unit source case; no fixture issuer, authorization or HTTP producer is replaced."""

import json
import unittest
from unittest.mock import patch
import subprocess

from journey_guard import JourneyBoundaryError, invoke_cli, literal_intent_key, redacted_plan, safe_base_url, sandbox_keys


class JourneyRedactionSourceCase(unittest.TestCase):
    def test_http_route_and_dry_run_defaults_never_publish_secrets_or_infer_original(self):
        private = "fixture-route-private-identity"
        plan = [{"entity": "lead", "identity": private,
                 "operations": {"current": {"method": "GET", "scope": "crm_leads:read",
                                            "route_template": "/v1/crm/leads/{lead}"}}}]
        result = redacted_plan("source-hash-only", plan)
        self.assertEqual(result["mode"], "dry-run")
        self.assertEqual(result["network_calls"], 0)
        self.assertEqual(result["writes"], 0)
        self.assertFalse(result["current_is_original"])
        self.assertNotIn(private, json.dumps(result))
        self.assertNotIn("argv", json.dumps(result))
        for base in ("https://secret@example.test", "https://example.test?key=private",
                     "https://example.test#private", "http://example.test", "https://example.test/v1"):
            with self.assertRaises(JourneyBoundaryError) as rejected:
                safe_base_url(base)
            self.assertNotIn(base, str(rejected.exception))
            self.assertNotIn("private", str(rejected.exception))
        self.assertEqual(safe_base_url("https://api.factuarea.com/"), "https://api.factuarea.com")
        self.assertEqual(safe_base_url("http://127.0.0.1:13900"), "http://127.0.0.1:13900")
        with self.assertRaises(JourneyBoundaryError):
            sandbox_keys({})
        with self.assertRaises(JourneyBoundaryError):
            sandbox_keys({"FACTUAREA_JOURNEY_A_KEY": "live-secret", "FACTUAREA_JOURNEY_B_KEY": "other-private"})

    def test_cli_http_boundary_passes_secrets_only_in_environment_and_literal_body_in_stdin(self):
        credential, key = "private-http-credential", "intent key internal spaces"
        raw = b'{\n "name":"Private human field"\n}\n'
        args = ["crm", "lead", "create", "--source-contracts", "--confirmed", "--data", "-",
                "--idempotency-key-env", "FACTUAREA_CRM_JOURNEY_INTENT"]
        with patch("journey_guard.subprocess.run") as runner:
            runner.return_value = subprocess.CompletedProcess([], 10, stdout=b"", stderr=b"private transport route")
            code, result = invoke_cli("factuarea", "http://127.0.0.1:14000", credential, args, {
                "FACTUAREA_JOURNEY_A_KEY": credential, "FACTUAREA_JOURNEY_B_KEY": "foreign-private",
            }, raw=raw, intent_key=key)
            self.assertEqual((code, result), (10, None))
            runner.assert_called_once()
            argv = runner.call_args.args[0]
            options = runner.call_args.kwargs
            for secret in (credential, key, "Private human field"):
                self.assertNotIn(secret, " ".join(argv))
            self.assertEqual(options["input"], raw)
            self.assertEqual(options["env"]["FACTUAREA_API_KEY"], credential)
            self.assertEqual(options["env"]["FACTUAREA_CRM_JOURNEY_INTENT"], key)
            self.assertNotIn("FACTUAREA_JOURNEY_B_KEY", options["env"])
        for value in ("", " space", "space ", "new\nline"):
            with self.assertRaises(JourneyBoundaryError):
                literal_intent_key(value)


if __name__ == "__main__":
    unittest.main()

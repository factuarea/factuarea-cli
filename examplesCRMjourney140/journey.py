#!/usr/bin/env python3
"""Human CRM journey through the native Source CLI, never a Python HTTP client."""

import argparse
import json
import os
import sys

from journey_guard import (
    JourneyBoundaryError, native_plan, read_sandboxes, redacted_plan,
    recover_original, run_journey, safe_base_url,
)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--native-input", required=True, help="Root's real native Source139 manifest")
    parser.add_argument("--source-contracts", action="store_true", help="Source opt-in; no release/grant")
    parser.add_argument("--confirmed", action="store_true", help="human authorizes journey; each write is reviewed again")
    for entity in ("lead", "opportunity", "activity"):
        parser.add_argument("--" + entity, help="existing native A UUID for --read")
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--dry-run", action="store_true", help="default: redacted plan, no CLI/network/writes")
    mode.add_argument("--read", action="store_true", help="Current A/B reads of an existing journey")
    mode.add_argument("--run", action="store_true", help="human Lead -> Opportunity -> Activity create/update")
    mode.add_argument("--recover", choices=("lead", "opportunity", "activity"), help="only GET create Original")
    parser.add_argument("--original-body-file", help="retained exact first create body for --recover")
    args = parser.parse_args(argv)
    try:
        if not args.source_contracts:
            raise JourneyBoundaryError("Se requiere Source opt-in explícito")
        native_hash, plan = native_plan(args.native_input, {
            "lead": args.lead, "opportunity": args.opportunity, "activity": args.activity,
        })
        base = safe_base_url(os.environ.get("FACTUAREA_BASE_URL", "https://api.factuarea.com"))
        binary = os.environ.get("FACTUAREA_CLI_BIN", "factuarea")
        if args.run:
            if not args.confirmed:
                raise JourneyBoundaryError("La escritura exige confirmación humana")
            result = run_journey(binary, base, plan)
        elif args.recover:
            result = recover_original(binary, base, args.recover, args.original_body_file)
        elif args.read:
            result = read_sandboxes(binary, base, plan)
        else:
            result = redacted_plan(native_hash, plan)
        result["native_sha256"] = native_hash
        print(json.dumps(result, ensure_ascii=False))
        return 0
    except (JourneyBoundaryError, OSError, ValueError, KeyError, EOFError):
        print(json.dumps({
            "state": "unconfirmed", "post_retry": False, "current_is_original": False,
            "message": "El paso no se acreditó. Conserva raw/key: create se recupera sólo con --recover; PATCH UNKNOWN no tiene Original público y no autoriza reenvío.",
        }), file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())

#!/usr/bin/env python3
"""Extract exact private operations and their reference closure from a native export.

This never rewrites the distributed OpenAPI or generates published commands.
"""

import argparse
import hashlib
import json
from pathlib import Path


SELECTED = {
    "import_apply_original": ("/crm/imports/{run}/apply/original", "get"),
    "agenda": ("/agenda", "get"),
}
BOUNDARIES = {
    "import_apply": ("/crm/imports/{run}/apply", "post"),
    "import_current": ("/crm/imports/{run}", "get"),
}


def extract(input_path, output_path):
    metadata = json.loads(input_path.read_bytes())
    native_bytes = Path(metadata["artifact"]).read_bytes()
    if hashlib.sha256(native_bytes).hexdigest() != metadata["artifact_sha256"]:
        raise ValueError("Native artifact does not match its input manifest")
    native = json.loads(native_bytes)
    profile = native["x-factuarea-contract-profile"]
    if profile["state"] != "unreleased" or any(
        metadata[name] is not False
        for name in ("ratified", "published", "delivery_enabled")
    ) or metadata["capabilities"] != []:
        raise ValueError("This extractor only accepts an unreleased, disabled Source contract")
    if any(profile[name] is not False for name in ("ratified", "published", "delivery_enabled")):
        raise ValueError("Native profile must retain its publication gate")

    def operations(selection):
        return {
            key: {"path": path, "method": method.upper(), "operation": native["paths"][path][method]}
            for key, (path, method) in selection.items()
        }

    selected = operations(SELECTED)
    boundaries = operations(BOUNDARIES)
    components = {}
    visited = set()

    def include(value):
        if isinstance(value, dict):
            ref = value.get("$ref")
            if ref is not None and ref not in visited:
                if not ref.startswith("#/components/"):
                    raise ValueError("External references are not admitted")
                visited.add(ref)
                _, _, group, name = ref.split("/", 3)
                component = native["components"][group][name]
                components.setdefault(group, {})[name] = component
                include(component)
            for child in value.values():
                include(child)
        elif isinstance(value, list):
            for child in value:
                include(child)

    include(selected)
    include(boundaries)
    selected_ids = {entry["operation"]["operationId"] for entry in selected.values()}
    result = {
        "source_artifact_sha256": metadata["artifact_sha256"],
        "source_manifest_sha256": hashlib.sha256(input_path.read_bytes()).hexdigest(),
        "native_profile": {key: value for key, value in profile.items() if key != "carriers"},
        "capabilities": metadata["capabilities"],
        "carriers": [c for c in profile["carriers"] if c["operation_id"] in selected_ids],
        "operations": selected,
        "boundaries": boundaries,
        "restore_paths_in_native_input": [p for p in native["paths"] if "documents" in p and "restore" in p],
        "components": components,
    }
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    arguments = parser.parse_args()
    extract(arguments.input, arguments.output)

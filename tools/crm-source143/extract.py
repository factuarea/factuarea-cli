#!/usr/bin/env python3
"""Select exact Source143 operations; reuse the mechanical native reference closure."""

import argparse
import importlib.util
from pathlib import Path


SELECTED = {
    "import_apply": ("/crm/imports/{run}/apply", "post"),
    "import_current": ("/crm/imports/{run}", "get"),
    "import_original": ("/crm/imports/{run}/apply/original", "get"),
    "document_restore": ("/crm/documents/{document}/restore", "post"),
    "document_current": ("/crm/documents/{document}", "get"),
}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    loader = importlib.util.spec_from_file_location(
        "native_reference_closure", Path(__file__).resolve().parents[1] / "crm-source139" / "extract.py",
    )
    source = importlib.util.module_from_spec(loader)
    loader.loader.exec_module(source)
    source.extract(args.input, args.output, selection=SELECTED, boundary_selection={})

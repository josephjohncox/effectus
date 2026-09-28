#!/usr/bin/env python3
"""Fail on govulncheck findings except one confirmed gRPC database error."""

import json
import sys


VULN_ID = "GO-2026-6443"
MODULE = "google.golang.org/grpc"
VERSION = "v1.84.0"


def parse_messages(report):
    decoder = json.JSONDecoder()
    messages = []
    offset = 0
    while offset < len(report):
        while offset < len(report) and report[offset].isspace():
            offset += 1
        if offset == len(report):
            break
        message, offset = decoder.raw_decode(report, offset)
        if not isinstance(message, dict):
            raise ValueError("govulncheck emitted a non-object message")
        messages.append(message)
    return messages


def assess(report):
    messages = parse_messages(report)
    expected_kinds = {"config", "SBOM", "progress", "osv", "finding"}
    if any(len(message) != 1 or not set(message).issubset(expected_kinds)
           for message in messages):
        raise ValueError("govulncheck emitted an unknown message type")
    configs = [message["config"] for message in messages if "config" in message]
    sboms = [message["SBOM"] for message in messages if "SBOM" in message]
    if len(configs) != 1 or any(configs[0].get(key) != value for key, value in {
            "scanner_name": "govulncheck",
            "protocol_version": "v1.0.0",
            "scan_mode": "source",
            "scan_level": "symbol",
    }.items()):
        raise ValueError("expected one source/symbol govulncheck configuration")
    if len(sboms) != 1 or not isinstance(sboms[0].get("modules"), list):
        raise ValueError("expected one govulncheck SBOM message")

    versions = [module.get("version") for module in sboms[0]["modules"]
                if module.get("path") == MODULE]
    exact_module = versions == [VERSION]
    suppressed = 0
    unexpected = []
    for message in messages:
        if "finding" not in message:
            continue
        finding = message["finding"]
        if not isinstance(finding, dict) or not isinstance(finding.get("osv"), str) \
                or not finding["osv"]:
            raise ValueError("govulncheck emitted a finding without an ID")
        trace = finding.get("trace")
        if not isinstance(trace, list) or not trace or not isinstance(trace[0], dict):
            raise ValueError("govulncheck emitted an invalid finding trace")
        first = trace[0]
        # Match the previous text-mode gate: source/symbol scans fail only
        # when the vulnerable function is called. Module and package findings
        # remain informational.
        if not first.get("function"):
            continue
        # GHSA-2v4p-qf9q-27wj lists v1.84.0 as patched. The Go database
        # incorrectly includes it; golang/vulndb#6580 proposes a correction.
        # Remove this exception once the corrected record is published.
        if (finding.get("osv") == VULN_ID and exact_module
                and first.get("module") == MODULE
                and first.get("version") == VERSION):
            suppressed += 1
        else:
            unexpected.append(finding)
    return suppressed, unexpected


def main():
    try:
        suppressed, unexpected = assess(sys.stdin.read())
    except (ValueError, TypeError, AttributeError) as error:
        print(f"invalid govulncheck report: {error}", file=sys.stderr)
        return 1
    if suppressed:
        print(f"Ignored {suppressed} {VULN_ID} finding(s) for {MODULE}@{VERSION}; "
              "upstream lists this release as patched")
    for finding in unexpected:
        first = (finding.get("trace") or [{}])[0]
        print(f"Vulnerability: {finding.get('osv', 'unknown')} "
              f"{first.get('module', 'unknown')}@{first.get('version', 'unknown')}",
              file=sys.stderr)
    if unexpected:
        return 1
    print("No other called vulnerabilities found")
    return 0


if __name__ == "__main__":
    sys.exit(main())

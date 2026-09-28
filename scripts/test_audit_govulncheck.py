import json
import unittest

from audit_govulncheck import assess


def report(*findings, version="v1.84.0", indent=None):
    messages = [
        {"config": {"scanner_name": "govulncheck", "protocol_version": "v1.0.0",
                    "scan_mode": "source", "scan_level": "symbol"}},
        {"SBOM": {"modules": [{"path": "google.golang.org/grpc", "version": version}]}},
    ]
    messages.extend({"finding": finding} for finding in findings)
    return "\n".join(json.dumps(message, indent=indent) for message in messages)


def finding(vuln="GO-2026-6443", version="v1.84.0", module="google.golang.org/grpc",
            function="HandleStreams"):
    return {"osv": vuln, "trace": [{"module": module, "version": version,
                                    "function": function}]}


class AuditGovulncheckTest(unittest.TestCase):
    def test_clean_report(self):
        self.assertEqual(assess(report()), (0, []))

    def test_only_exact_false_positive_is_ignored(self):
        self.assertEqual(assess(report(finding())), (1, []))
        self.assertEqual(assess(report(finding(), indent=2)), (1, []))
        other = finding("GO-2026-9999")
        self.assertEqual(assess(report(finding(), other)), (1, [other]))

    def test_version_or_module_changes_restore_the_failure(self):
        for candidate in (
            report(finding(), version="v1.84.1"),
            report(finding(version="v1.84.1")),
            report(finding(module="example.org/other")),
        ):
            with self.subTest(candidate=candidate):
                self.assertEqual(assess(candidate)[0], 0)
                self.assertEqual(len(assess(candidate)[1]), 1)

    def test_uncalled_findings_preserve_previous_text_mode_gate(self):
        self.assertEqual(assess(report(finding("GO-2026-9999", function=""))), (0, []))

    def test_scan_schema_or_level_changes_fail(self):
        base = report()
        for candidate in (
            base.replace('"scan_level": "symbol"', '"scan_level": "package"'),
            base.replace('"protocol_version": "v1.0.0"', '"protocol_version": "v2.0.0"'),
            base + "\n" + json.dumps({"error": "scan failed"}),
        ):
            with self.subTest(candidate=candidate):
                with self.assertRaises(ValueError):
                    assess(candidate)

    def test_incomplete_or_malformed_scan_fails(self):
        for candidate in ("", "{", json.dumps({"finding": finding()}),
                          report({"trace": [{"module": "example.org/other"}]}),
                          report({"osv": "GO-2026-9999", "trace": []})):
            with self.subTest(candidate=candidate):
                with self.assertRaises((ValueError, json.JSONDecodeError)):
                    assess(candidate)


if __name__ == "__main__":
    unittest.main()

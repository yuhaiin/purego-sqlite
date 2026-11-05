"""Regression tests for the benchmark report's visible and paired baselines."""

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


class CompareTests(unittest.TestCase):
    def report(self, runs):
        with tempfile.TemporaryDirectory() as directory:
            arguments = []
            for backend, samples in runs.items():
                path = Path(directory) / f"{backend}.txt"
                path.write_text("".join(
                    f"BenchmarkSQLite/LargeTextWrite/{driver}-4 100 {value} ns/op 16 B/op 2 allocs/op\n"
                    for driver, values in samples.items() for value in values
                ))
                arguments.append(f"{backend}={path}")
            return subprocess.check_output(
                [sys.executable, str(Path(__file__).with_name("compare.py")), *arguments],
                text=True,
            )

    def test_visible_ratios_use_pooled_mattn_with_skewed_run_controls(self):
        report = self.report({
            "purego": {"purego": [90000], "mattn": [90000, 190000, 500000], "modernc": [100000]},
            "purego-bridge": {"purego": [91000], "mattn": [87000, 88000, 89000], "modernc": [110000]},
        })
        # Pooling six controls yields 89,500, rather than either run's median.
        time_table = report.split("### Go memory:")[0]
        self.assertIn("| LargeTextWrite | 90,000 (1.01x) | 91,000 (1.02x) | 89,500 | 105,000 (1.17x) |", time_table)
        self.assertIn("### Native time / matching-run mattn", report)
        self.assertIn("| LargeTextWrite | 0.47x (190,000) | 1.03x (88,000) |", report)
        self.assertIn("| LargeTextWrite | 0.90x (100,000) | 0.83x (110,000) |", report)

    def test_single_run_has_same_visible_and_matching_baseline(self):
        report = self.report({"purego": {"purego": [120], "mattn": [100]}})
        self.assertIn("| LargeTextWrite | 120 (1.20x) | 100 |", report)
        self.assertIn("| LargeTextWrite | 1.20x (100) |", report)
        self.assertNotIn("### Native time / matching-run modernc", report)

    def test_missing_mattn_does_not_invent_a_ratio(self):
        report = self.report({"purego": {"purego": [120]}})
        self.assertIn("| LargeTextWrite | 120 |", report)
        self.assertNotIn("(1.00x)", report)
        self.assertNotIn("### Native time / matching-run mattn", report)


if __name__ == "__main__":
    unittest.main()

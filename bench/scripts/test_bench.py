import json
import unittest
from unittest.mock import patch

from bench_common import count_matches, match_rows, redact
import bench_lang_rg_vs_codefind as lang
import bench_rg_vs_codefind as xlsx


class BenchTests(unittest.TestCase):
    def test_single_file_counts(self):
        self.assertEqual(count_matches("7\n", 0), 7)
        self.assertEqual(count_matches("C:\\file:7\nother:2\n", 0), 9)
        self.assertEqual(count_matches("7\n", 2), 0)

    def test_windows_noise(self):
        result = lang.analyze_rg("C:\\repo\\vendor\\noise.go:12:Target\n", 0)
        self.assertEqual(result["noise_hits"], 1)
        self.assertEqual(result["sample_paths"], ["C:\\repo\\vendor\\noise.go"])

    def test_error_is_not_hit(self):
        self.assertEqual(lang.analyze_rg("rg: invalid option: --bad\n", 2)["hit_count"], 0)
        with patch.object(lang, "run_cmd", return_value=(2, "", "rg: error: bad", 1.2)):
            summary, runs = lang.timed_runs("rg", [], lang.analyze_rg, "q", "neg", 1)
        self.assertEqual(summary["status"], "rg_ec_2")
        self.assertEqual(runs[0]["stderr"], "rg: error: bad")

    def test_recursive_path_redaction(self):
        value = {"runs": [{"argv": ["C:\\Users\\person\\project\\tool.exe"], "stderr": "at C:\\Users\\person\\project\\file.go"}]}
        safe = json.dumps(redact(value, [("C:\\Users\\person\\project", "<REPO>")]))
        self.assertNotIn("person", safe)
        self.assertIn("<REPO>", safe)

    def test_environment_isolation_and_precision(self):
        self.assertIn("--no-config", lang.rg_argv("x", naive=True, langs=None))
        self.assertEqual(lang.median_ms([1.1, 1.2, 1.3]), 1.2)
        self.assertEqual(xlsx.median_ms([1.1, 1.2, 1.3]), 1.2)


if __name__ == "__main__":
    unittest.main()

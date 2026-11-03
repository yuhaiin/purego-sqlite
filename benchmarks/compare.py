#!/usr/bin/env python3
"""Generate a median comparison table from go test -bench results (no deps)."""
import re
import statistics
import sys
from collections import defaultdict

results = defaultdict(lambda: defaultdict(list))
for line in open(sys.argv[1]):
    match = re.match(r"BenchmarkSQLite/([^/]+)/(purego|mattn)-\d+\s+\d+\s+(.*)", line)
    if not match:
        continue
    name, driver, raw = match.groups()
    metrics = dict((unit, float(value)) for value, unit in re.findall(r"([\d.]+)\s+(ns/op|B/op|allocs/op|ops/s)", raw))
    results[name][driver].append(metrics)

print("Medians of repeated samples. Ratio = PureGo time / mattn time (lower is better).")
print()
print("| Workload | PureGo ns/op | mattn ns/op | Ratio | PureGo B/op | mattn B/op | PureGo allocs/op | mattn allocs/op | PureGo ops/s | mattn ops/s |")
print("|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|")
for name, implementations in results.items():
    def med(driver, metric):
        return statistics.median(row[metric] for row in implementations[driver])
    p, m = med("purego", "ns/op"), med("mattn", "ns/op")
    values = [f"{p:,.0f}", f"{m:,.0f}", f"{p/m:.2f}x"]
    for metric in ("B/op", "allocs/op", "ops/s"):
        values.extend(f"{med(driver,metric):,.0f}" for driver in ("purego", "mattn"))
    print("| " + name + " | " + " | ".join(values) + " |")
if not results:
    raise SystemExit("no benchmark data")

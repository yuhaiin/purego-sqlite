#!/usr/bin/env python3
"""Median tables: compare.py [purego=|cgo=|cgo-dynamic=]RAW_LOG ...

Combining all three runs produces five implementation columns. mattn/modernc
controls are pooled across the input runs; sample counts are printed explicitly.
No dependencies required.
"""
import re
import statistics
import sys
from collections import defaultdict
from pathlib import Path

LABELS = {
    "purego": "PureGo",
    "cgo": "CGO",
    "cgo-static": "CGO static",
    "cgo-dynamic": "CGO dynamic",
    "mattn": "mattn",
    "modernc": "modernc.org/sqlite",
}
ORDER = ("purego", "cgo-dynamic", "cgo-static", "cgo", "mattn", "modernc")
results = defaultdict(lambda: defaultdict(list))
for specification in sys.argv[1:]:
    backend, separator, path = specification.partition("=")
    if not separator:
        backend, path = "", specification
    elif backend not in ("purego", "cgo", "cgo-static", "cgo-dynamic"):
        raise SystemExit(f"unknown backend label: {backend}")
    for line in Path(path).read_text().splitlines():
        match = re.match(r"BenchmarkSQLite/([^/]+)/(purego|cgo|mattn|modernc)-\d+\s+\d+\s+(.*)", line)
        if not match:
            continue
        name, driver, raw = match.groups()
        if backend and driver in ("purego", "cgo"):
            driver = "cgo-static" if backend == "cgo" else backend
        metrics = {unit: float(value) for value, unit in re.findall(r"([\d.]+)\s+(ns/op|B/op|allocs/op|ops/s)", raw)}
        results[name][driver].append(metrics)

if not results:
    raise SystemExit("no benchmark data")
drivers = [d for d in ORDER if any(d in group for group in results.values())]


def median(name, driver, metric):
    values = [row[metric] for row in results[name][driver] if metric in row]
    return statistics.median(values) if values else None


print("Medians of repeated samples. Time ratio = implementation / mattn (lower is better).")
print("When logs are combined, mattn and modernc controls are pooled across runs.")
print()
for driver in drivers:
    counts = [len(group.get(driver, [])) for group in results.values()]
    count = str(min(counts)) if min(counts) == max(counts) else f"{min(counts)}–{max(counts)}"
    print(f"- {LABELS[driver]}: {count} samples per workload")
print()
for title, metrics in (("Time: ns/op (ratio to mattn)", ("ns/op",)),
                       ("Go memory: B/op / allocs/op", ("B/op", "allocs/op")),
                       ("Throughput: benchmark units/s", ("ops/s",))):
    print(f"### {title}\n")
    print("| Workload | " + " | ".join(LABELS[d] for d in drivers) + " |")
    print("|---|" + "---:|" * len(drivers))
    for name in results:
        values = []
        for driver in drivers:
            measurements = [median(name, driver, metric) for metric in metrics]
            if any(v is None for v in measurements):
                values.append("—")
                continue
            value = " / ".join(f"{v:,.0f}" for v in measurements)
            if metrics == ("ns/op",) and driver != "mattn":
                reference = median(name, "mattn", "ns/op")
                if reference:
                    value += f" ({measurements[0]/reference:.2f}x)"
            values.append(value)
        print("| " + name + " | " + " | ".join(values) + " |")
    print()

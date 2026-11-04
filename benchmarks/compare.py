#!/usr/bin/env python3
"""Median tables: compare.py [purego=|cgo=|cgo-dynamic=]RAW_LOG ...

Combining all three runs produces five implementation columns. mattn/modernc
controls are pooled across the input runs; native time ratios use matching-run
mattn controls to avoid confusing cross-run load shifts with driver overhead.
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
references = defaultdict(lambda: defaultdict(list))
for specification in sys.argv[1:]:
    backend, separator, path = specification.partition("=")
    if not separator:
        backend, path = "", specification
    elif backend not in ("purego", "cgo", "cgo-static", "cgo-dynamic"):
        raise SystemExit(f"unknown backend label: {backend}")
    local = defaultdict(lambda: defaultdict(list))
    for line in Path(path).read_text().splitlines():
        match = re.match(r"BenchmarkSQLite/([^/]+)/(purego|cgo|mattn|modernc)-\d+\s+\d+\s+(.*)", line)
        if not match:
            continue
        name, driver, raw = match.groups()
        if backend and driver in ("purego", "cgo"):
            driver = "cgo-static" if backend == "cgo" else backend
        metrics = {unit: float(value) for value, unit in re.findall(r"([\d.]+)\s+(ns/op|B/op|allocs/op|ops/s)", raw)}
        local[name][driver].append(metrics)
    for name, group in local.items():
        for driver, rows in group.items():
            results[name][driver].extend(rows)
            references[name][driver].extend(group.get("mattn", []))

if not results:
    raise SystemExit("no benchmark data")
drivers = [d for d in ORDER if any(d in group for group in results.values())]


def median(name, driver, metric):
    values = [row[metric] for row in results[name][driver] if metric in row]
    return statistics.median(values) if values else None


print("Medians of repeated samples. Time ratio = implementation / matching-run mattn (lower is better).")
print("mattn and modernc columns pool controls across runs; each native backend's ratio uses its own run's mattn median.")
print()
for driver in drivers:
    counts = [len(group.get(driver, [])) for group in results.values()]
    count = str(min(counts)) if min(counts) == max(counts) else f"{min(counts)}–{max(counts)}"
    print(f"- {LABELS[driver]}: {count} samples per workload")
print()
for title, metrics in (("Time: ns/op (ratio to matching-run mattn)", ("ns/op",)),
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
                controls = references[name][driver]
                reference = statistics.median(row["ns/op"] for row in controls) if controls else None
                if reference:
                    value += f" ({measurements[0]/reference:.2f}x)"
            values.append(value)
        print("| " + name + " | " + " | ".join(values) + " |")
    print()

native = [d for d in drivers if d not in ("mattn", "modernc")]
if len(native) > 1 and any(references[name][d] for name in results for d in native):
    print("### Matching mattn controls: ns/op\n")
    print("These medians are the denominators for the native backend ratios above.\n")
    print("| Workload | " + " | ".join(LABELS[d] + " run" for d in native) + " |")
    print("|---|" + "---:|" * len(native))
    for name in results:
        values = []
        for driver in native:
            controls = references[name][driver]
            values.append(f"{statistics.median(row['ns/op'] for row in controls):,.0f}" if controls else "—")
        print("| " + name + " | " + " | ".join(values) + " |")

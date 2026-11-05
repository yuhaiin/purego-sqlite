#!/usr/bin/env python3
"""Median tables: compare.py [BACKEND=]RAW_LOG ...

Add purego-bridge/cgo-dynamic-bridge runs to compare optional row readers beside
the standard API paths. Combining three runs produces five columns; combining
both API paths and static CGO produces seven columns. mattn/modernc
controls are pooled across the input runs. The main time table uses its displayed
mattn median for every ratio. Separate paired tables show matching-run comparisons
with their actual denominators so cross-run load shifts remain visible.
No dependencies required.
"""
import re
import statistics
import sys
from collections import defaultdict
from pathlib import Path

LABELS = {
    "purego": "PureGo",
    "purego-bridge": "PureGo embed",
    "cgo": "CGO",
    "cgo-static": "CGO static",
    "cgo-dynamic": "CGO dynamic",
    "cgo-dynamic-bridge": "CGO dynamic embed",
    "mattn": "mattn",
    "modernc": "modernc.org/sqlite",
}
ORDER = ("purego", "purego-bridge", "cgo-dynamic", "cgo-dynamic-bridge", "cgo-static", "cgo", "mattn", "modernc")
results = defaultdict(lambda: defaultdict(list))
references = defaultdict(lambda: defaultdict(list))
modernc_references = defaultdict(lambda: defaultdict(list))
for specification in sys.argv[1:]:
    backend, separator, path = specification.partition("=")
    if not separator:
        backend, path = "", specification
    elif backend not in ("purego", "purego-bridge", "cgo", "cgo-static", "cgo-dynamic", "cgo-dynamic-bridge"):
        raise SystemExit(f"unknown backend label: {backend}")
    local = defaultdict(lambda: defaultdict(list))
    for line in Path(path).read_text().splitlines():
        match = re.match(r"Benchmark(?:SQLite|MattnParity|MattnMedium)/(.+)/(purego|cgo|mattn|modernc)-\d+\s+\d+\s+(.*)", line)
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
            modernc_references[name][driver].extend(group.get("modernc", []))

if not results:
    raise SystemExit("no benchmark data")
drivers = [d for d in ORDER if any(d in group for group in results.values())]
native = [d for d in drivers if d not in ("mattn", "modernc")]
if "purego-bridge" in drivers:
    LABELS["purego"] = "PureGo standard"
if "cgo-dynamic-bridge" in drivers:
    LABELS["cgo-dynamic"] = "CGO dynamic standard"


def median(name, driver, metric):
    values = [row[metric] for row in results[name][driver] if metric in row]
    return statistics.median(values) if values else None


print("Medians of repeated samples. Main-table time ratio = implementation / displayed mattn median (lower is better).")
print("mattn and modernc columns pool controls across runs; separate matching-run tables show paired ratios and their denominators.")
print()
for driver in drivers:
    counts = [len(group.get(driver, [])) for group in results.values()]
    count = str(min(counts)) if min(counts) == max(counts) else f"{min(counts)}–{max(counts)}"
    print(f"- {LABELS[driver]}: {count} samples per workload")
print()
for title, metrics in (("Time: ns/op (ratio to displayed mattn)", ("ns/op",)),
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

if any(modernc_references[name][d] for name in results for d in native):
    print("### Native time / matching-run modernc\n")
    print("Each cell shows the time ratio and matching modernc median in ns/op; lower ratios are better.\n")
    print("| Workload | " + " | ".join(LABELS[d] for d in native) + " |")
    print("|---|" + "---:|" * len(native))
    for name in results:
        values = []
        for driver in native:
            controls = modernc_references[name][driver]
            current = median(name, driver, "ns/op")
            if not controls or current is None:
                values.append("—")
                continue
            reference = statistics.median(row['ns/op'] for row in controls)
            values.append(f"{current/reference:.2f}x ({reference:,.0f})")
        print("| " + name + " | " + " | ".join(values) + " |")
    print()

if any(references[name][d] for name in results for d in native):
    print("### Native time / matching-run mattn\n")
    print("Each cell shows the time ratio and matching mattn median in ns/op; these denominators can differ from the main table's pooled median.\n")
    print("| Workload | " + " | ".join(LABELS[d] for d in native) + " |")
    print("|---|" + "---:|" * len(native))
    for name in results:
        values = []
        for driver in native:
            controls = references[name][driver]
            current = median(name, driver, "ns/op")
            if not controls or current is None:
                values.append("—")
                continue
            reference = statistics.median(row["ns/op"] for row in controls)
            values.append(f"{current/reference:.2f}x ({reference:,.0f})")
        print("| " + name + " | " + " | ".join(values) + " |")

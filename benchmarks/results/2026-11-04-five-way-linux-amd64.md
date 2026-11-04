Medians of repeated samples. Time ratio = implementation / matching-run mattn (lower is better).
mattn and modernc columns pool controls across runs; each native backend's ratio uses its own run's mattn median.

- PureGo: 5 samples per workload
- CGO dynamic: 5 samples per workload
- CGO static: 5 samples per workload
- mattn: 15 samples per workload
- modernc.org/sqlite: 15 samples per workload

### Time: ns/op (ratio to matching-run mattn)

| Workload | PureGo | CGO dynamic | CGO static | mattn | modernc.org/sqlite |
|---|---:|---:|---:|---:|---:|
| OpenClose | 73,861 (0.97x) | 64,772 (0.86x) | 64,820 (0.86x) | 75,026 | 113,703 (1.52x) |
| SelectConstant | 3,026 (1.07x) | 2,560 (0.93x) | 2,566 (0.92x) | 2,800 | 2,875 (1.03x) |
| SingleInsert | 11,674 (1.04x) | 11,214 (1.00x) | 10,893 (0.98x) | 11,184 | 14,560 (1.30x) |
| PreparedInsert | 9,587 (1.06x) | 9,279 (1.02x) | 9,156 (1.01x) | 9,053 | 11,625 (1.28x) |
| BatchInsert1000 | 1,073,860 (1.36x) | 731,747 (0.93x) | 722,785 (0.95x) | 785,633 | 1,031,094 (1.31x) |
| BatchInsert1000Values | 299,306 (1.24x) | 246,959 (1.00x) | 244,759 (1.01x) | 242,045 | 426,239 (1.76x) |
| TransactionInsert100000 | 107,865,040 (1.39x) | 72,119,588 (0.93x) | 71,940,162 (0.95x) | 77,246,321 | 102,853,750 (1.33x) |
| SingleRowSelect | 8,278 (1.10x) | 7,188 (0.96x) | 7,116 (0.96x) | 7,513 | 10,317 (1.37x) |
| FullScan1000 | 1,176,862 (1.98x) | 742,007 (1.24x) | 700,569 (1.22x) | 594,134 | 554,980 (0.93x) |
| IndexedLookup | 8,283 (1.06x) | 7,298 (0.98x) | 7,376 (0.94x) | 7,761 | 11,799 (1.52x) |
| LargeTextRead | 147,013 (0.98x) | 159,411 (1.15x) | 155,590 (1.19x) | 138,165 | 163,250 (1.18x) |
| LargeTextWrite | 81,437 (1.02x) | 80,865 (1.01x) | 80,105 (1.00x) | 80,158 | 109,424 (1.37x) |
| LargeBlobRead | 419,996 (1.01x) | 423,993 (1.04x) | 411,154 (0.99x) | 413,358 | 418,906 (1.01x) |
| LargeBlobWrite | 86,402 (1.07x) | 80,910 (1.00x) | 80,339 (0.99x) | 81,053 | 109,586 (1.35x) |
| WALReaders | 2,009 (0.96x) | 2,017 (0.97x) | 2,025 (0.95x) | 2,112 | 3,328 (1.58x) |
| WALWriterContention | 13,481 (1.15x) | 12,398 (0.96x) | 12,686 (1.08x) | 12,095 | 19,119 (1.58x) |
| FTS5Search | 40,023 (1.00x) | 38,478 (0.98x) | 39,450 (0.98x) | 39,747 | 83,559 (2.10x) |
| JSONQuery | 4,866 (1.08x) | 4,150 (0.96x) | 4,144 (0.96x) | 4,341 | 5,662 (1.30x) |

### Go memory: B/op / allocs/op

| Workload | PureGo | CGO dynamic | CGO static | mattn | modernc.org/sqlite |
|---|---:|---:|---:|---:|---:|
| OpenClose | 4,234 / 95 | 2,503 / 36 | 2,484 / 36 | 2,977 / 64 | 4,583 / 83 |
| SelectConstant | 520 / 15 | 472 / 15 | 472 / 15 | 536 / 14 | 480 / 13 |
| SingleInsert | 280 / 8 | 232 / 7 | 232 / 7 | 224 / 7 | 124 / 5 |
| PreparedInsert | 112 / 4 | 112 / 4 | 112 / 4 | 128 / 5 | 116 / 5 |
| BatchInsert1000 | 125,201 / 5,516 | 125,018 / 5,513 | 125,018 / 5,513 | 140,780 / 6,508 | 131,645 / 6,502 |
| BatchInsert1000Values | 42,295 / 32 | 42,137 / 29 | 42,135 / 29 | 41,901 / 25 | 41,786 / 19 |
| TransactionInsert100000 | 12,799,917 / 599,527 | 12,797,017 / 599,513 | 12,797,020 / 599,513 | 14,396,780 / 699,508 | 13,595,650 / 699,502 |
| SingleRowSelect | 704 / 24 | 640 / 23 | 640 / 23 | 736 / 26 | 672 / 24 |
| FullScan1000 | 82,473 / 7,304 | 82,408 / 7,303 | 82,408 / 7,303 | 82,520 / 7,308 | 82,440 / 7,305 |
| IndexedLookup | 640 / 20 | 536 / 18 | 536 / 18 | 616 / 19 | 568 / 19 |
| LargeTextRead | 1,049,401 / 18 | 1,049,082 / 17 | 1,049,082 / 17 | 1,049,139 / 16 | 1,049,091 / 16 |
| LargeTextWrite | 160 / 5 | 112 / 4 | 112 / 4 | 128 / 5 | 132 / 6 |
| LargeBlobRead | 2,098,286 / 21 | 2,097,689 / 18 | 2,097,687 / 18 | 2,097,744 / 17 | 2,097,692 / 17 |
| LargeBlobWrite | 160 / 5 | 112 / 4 | 112 / 4 | 128 / 5 | 132 / 6 |
| WALReaders | 544 / 18 | 496 / 17 | 496 / 17 | 568 / 18 | 512 / 17 |
| WALWriterContention | 232 / 7 | 184 / 6 | 184 / 6 | 72 / 4 | 76 / 4 |
| FTS5Search | 640 / 20 | 544 / 18 | 544 / 18 | 600 / 17 | 568 / 18 |
| JSONQuery | 648 / 18 | 552 / 16 | 552 / 16 | 608 / 15 | 576 / 16 |

### Throughput: benchmark units/s

| Workload | PureGo | CGO dynamic | CGO static | mattn | modernc.org/sqlite |
|---|---:|---:|---:|---:|---:|
| OpenClose | 13,539 | 15,439 | 15,427 | 13,329 | 8,795 |
| SelectConstant | 330,439 | 390,595 | 389,753 | 357,122 | 347,795 |
| SingleInsert | 85,662 | 89,171 | 91,799 | 89,417 | 68,680 |
| PreparedInsert | 104,312 | 107,772 | 109,218 | 110,458 | 86,020 |
| BatchInsert1000 | 931 | 1,367 | 1,384 | 1,273 | 970 |
| BatchInsert1000Values | 3,341 | 4,049 | 4,086 | 4,131 | 2,346 |
| TransactionInsert100000 | 9 | 14 | 14 | 13 | 10 |
| SingleRowSelect | 120,800 | 139,127 | 140,538 | 133,095 | 96,928 |
| FullScan1000 | 850 | 1,348 | 1,427 | 1,683 | 1,802 |
| IndexedLookup | 120,725 | 137,031 | 135,583 | 128,844 | 84,750 |
| LargeTextRead | 6,802 | 6,273 | 6,427 | 7,238 | 6,126 |
| LargeTextWrite | 12,279 | 12,366 | 12,484 | 12,475 | 9,139 |
| LargeBlobRead | 2,381 | 2,359 | 2,432 | 2,419 | 2,387 |
| LargeBlobWrite | 11,574 | 12,359 | 12,447 | 12,338 | 9,125 |
| WALReaders | 497,741 | 495,813 | 493,877 | 473,516 | 300,457 |
| WALWriterContention | 74,180 | 80,659 | 78,825 | 82,676 | 52,305 |
| FTS5Search | 24,985 | 25,989 | 25,348 | 25,159 | 11,968 |
| JSONQuery | 205,512 | 240,982 | 241,309 | 230,337 | 176,602 |

### Native time / matching-run modernc

Each cell shows the time ratio and matching modernc median in ns/op; lower ratios are better.

| Workload | PureGo | CGO dynamic | CGO static |
|---|---:|---:|---:|
| OpenClose | 0.66x (111,196) | 0.57x (114,573) | 0.57x (113,703) |
| SelectConstant | 1.05x (2,873) | 0.89x (2,863) | 0.89x (2,877) |
| SingleInsert | 0.81x (14,473) | 0.76x (14,736) | 0.76x (14,349) |
| PreparedInsert | 0.83x (11,592) | 0.78x (11,872) | 0.79x (11,609) |
| BatchInsert1000 | 1.06x (1,008,861) | 0.71x (1,031,094) | 0.67x (1,071,368) |
| BatchInsert1000Values | 0.69x (433,130) | 0.58x (423,951) | 0.58x (424,767) |
| TransactionInsert100000 | 1.06x (101,411,038) | 0.70x (102,839,921) | 0.67x (107,407,740) |
| SingleRowSelect | 0.83x (10,026) | 0.70x (10,286) | 0.67x (10,553) |
| FullScan1000 | 2.12x (554,044) | 1.19x (622,248) | 1.27x (553,664) |
| IndexedLookup | 0.73x (11,294) | 0.62x (11,828) | 0.61x (12,114) |
| LargeTextRead | 0.90x (163,719) | 0.97x (164,839) | 0.96x (161,387) |
| LargeTextWrite | 0.76x (107,420) | 0.74x (109,459) | 0.72x (110,672) |
| LargeBlobRead | 1.00x (421,242) | 1.02x (414,175) | 1.00x (412,767) |
| LargeBlobWrite | 0.81x (106,557) | 0.74x (109,641) | 0.72x (111,455) |
| WALReaders | 0.64x (3,163) | 0.60x (3,376) | 0.60x (3,352) |
| WALWriterContention | 0.65x (20,766) | 0.64x (19,251) | 0.68x (18,585) |
| FTS5Search | 0.48x (83,157) | 0.47x (82,365) | 0.47x (83,885) |
| JSONQuery | 0.86x (5,662) | 0.75x (5,499) | 0.72x (5,716) |

### Matching mattn controls: ns/op

These medians are the denominators for the native backend ratios above.

| Workload | PureGo run | CGO dynamic run | CGO static run |
|---|---:|---:|---:|
| OpenClose | 75,756 | 75,026 | 74,965 |
| SelectConstant | 2,825 | 2,762 | 2,800 |
| SingleInsert | 11,188 | 11,208 | 11,165 |
| PreparedInsert | 9,003 | 9,069 | 9,053 |
| BatchInsert1000 | 787,615 | 787,206 | 759,237 |
| BatchInsert1000Values | 241,083 | 247,923 | 242,045 |
| TransactionInsert100000 | 77,766,240 | 77,246,321 | 75,362,496 |
| SingleRowSelect | 7,546 | 7,476 | 7,446 |
| FullScan1000 | 593,986 | 597,865 | 575,315 |
| IndexedLookup | 7,800 | 7,459 | 7,847 |
| LargeTextRead | 150,143 | 138,165 | 131,107 |
| LargeTextWrite | 79,938 | 80,158 | 80,212 |
| LargeBlobRead | 415,095 | 407,107 | 414,915 |
| LargeBlobWrite | 81,067 | 81,053 | 80,791 |
| WALReaders | 2,097 | 2,077 | 2,127 |
| WALWriterContention | 11,730 | 12,912 | 11,769 |
| FTS5Search | 39,971 | 39,188 | 40,056 |
| JSONQuery | 4,489 | 4,306 | 4,337 |

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
| OpenClose | 79,569 (0.98x) | 67,441 (0.88x) | 65,538 (0.88x) | 76,245 | 117,241 (1.54x) |
| SelectConstant | 3,112 (1.07x) | 2,567 (0.92x) | 2,563 (0.91x) | 2,807 | 2,909 (1.04x) |
| SingleInsert | 11,828 (1.05x) | 10,969 (1.00x) | 10,984 (0.99x) | 11,099 | 14,420 (1.30x) |
| PreparedInsert | 9,600 (1.07x) | 9,152 (1.02x) | 8,924 (1.00x) | 8,980 | 11,571 (1.29x) |
| BatchInsert1000 | 1,120,592 (1.31x) | 734,101 (0.93x) | 717,278 (0.96x) | 789,456 | 1,004,860 (1.27x) |
| BatchInsert1000Values | 297,544 (1.23x) | 245,738 (1.01x) | 242,500 (1.00x) | 241,825 | 429,549 (1.78x) |
| TransactionInsert100000 | 111,401,667 (1.35x) | 72,941,950 (0.92x) | 69,712,722 (0.95x) | 78,963,517 | 100,786,062 (1.28x) |
| SingleRowSelect | 7,872 (1.03x) | 7,041 (0.95x) | 7,026 (0.95x) | 7,427 | 10,318 (1.39x) |
| FullScan1000 | 712,731 (1.18x) | 564,454 (1.01x) | 689,726 (1.24x) | 563,593 | 552,132 (0.98x) |
| IndexedLookup | 8,399 (1.03x) | 7,393 (0.97x) | 7,235 (0.95x) | 7,661 | 11,485 (1.50x) |
| LargeTextRead | 155,661 (1.04x) | 157,532 (1.14x) | 155,835 (1.10x) | 145,045 | 168,299 (1.16x) |
| LargeTextWrite | 80,497 (1.01x) | 79,226 (1.00x) | 80,594 (1.00x) | 79,980 | 105,216 (1.32x) |
| LargeBlobRead | 302,034 (1.02x) | 422,449 (1.03x) | 256,190 (1.01x) | 294,975 | 313,470 (1.06x) |
| LargeBlobWrite | 80,301 (1.00x) | 80,860 (1.00x) | 80,030 (1.00x) | 80,196 | 106,187 (1.32x) |
| WALReaders | 2,103 (0.95x) | 2,014 (0.95x) | 1,999 (0.96x) | 2,130 | 3,545 (1.66x) |
| WALWriterContention | 16,002 (0.94x) | 14,680 (1.30x) | 12,645 (1.04x) | 12,956 | 20,036 (1.55x) |
| FTS5Search | 70,992 (1.04x) | 46,084 (1.01x) | 38,828 (0.96x) | 45,490 | 82,663 (1.82x) |
| JSONQuery | 4,885 (1.08x) | 4,092 (0.95x) | 4,163 (0.96x) | 4,327 | 5,799 (1.34x) |

### Go memory: B/op / allocs/op

| Workload | PureGo | CGO dynamic | CGO static | mattn | modernc.org/sqlite |
|---|---:|---:|---:|---:|---:|
| OpenClose | 4,343 / 97 | 2,552 / 36 | 2,531 / 36 | 2,970 / 64 | 4,597 / 83 |
| SelectConstant | 520 / 15 | 472 / 15 | 472 / 15 | 536 / 14 | 480 / 13 |
| SingleInsert | 280 / 8 | 232 / 7 | 232 / 7 | 224 / 7 | 124 / 5 |
| PreparedInsert | 112 / 4 | 112 / 4 | 112 / 4 | 128 / 5 | 116 / 5 |
| BatchInsert1000 | 125,203 / 5,516 | 125,026 / 5,513 | 125,024 / 5,513 | 140,779 / 6,508 | 131,647 / 6,502 |
| BatchInsert1000Values | 42,292 / 32 | 42,134 / 29 | 42,133 / 29 | 41,899 / 25 | 41,790 / 19 |
| TransactionInsert100000 | 12,799,837 / 599,526 | 12,797,081 / 599,513 | 12,797,017 / 599,513 | 14,396,785 / 699,508 | 13,595,698 / 699,502 |
| SingleRowSelect | 704 / 24 | 640 / 23 | 640 / 23 | 736 / 26 | 672 / 24 |
| FullScan1000 | 82,475 / 7,304 | 82,408 / 7,303 | 82,408 / 7,303 | 82,520 / 7,308 | 82,440 / 7,305 |
| IndexedLookup | 640 / 20 | 536 / 18 | 536 / 18 | 616 / 19 | 568 / 19 |
| LargeTextRead | 1,049,419 / 19 | 1,049,082 / 17 | 1,049,084 / 17 | 1,049,139 / 16 | 1,049,090 / 16 |
| LargeTextWrite | 160 / 5 | 112 / 4 | 112 / 4 | 128 / 5 | 132 / 6 |
| LargeBlobRead | 2,098,279 / 21 | 2,097,689 / 18 | 2,097,684 / 18 | 2,097,741 / 17 | 2,097,692 / 17 |
| LargeBlobWrite | 160 / 5 | 112 / 4 | 112 / 4 | 128 / 5 | 132 / 6 |
| WALReaders | 544 / 18 | 496 / 17 | 496 / 17 | 568 / 18 | 512 / 17 |
| WALWriterContention | 232 / 7 | 184 / 6 | 184 / 6 | 72 / 4 | 76 / 4 |
| FTS5Search | 640 / 20 | 544 / 18 | 544 / 18 | 600 / 17 | 568 / 18 |
| JSONQuery | 648 / 18 | 552 / 16 | 552 / 16 | 608 / 15 | 576 / 16 |

### Throughput: benchmark units/s

| Workload | PureGo | CGO dynamic | CGO static | mattn | modernc.org/sqlite |
|---|---:|---:|---:|---:|---:|
| OpenClose | 12,568 | 14,828 | 15,258 | 13,116 | 8,529 |
| SelectConstant | 321,379 | 389,527 | 390,219 | 356,212 | 343,811 |
| SingleInsert | 84,545 | 91,164 | 91,038 | 90,094 | 69,348 |
| PreparedInsert | 104,172 | 109,265 | 112,052 | 111,362 | 86,420 |
| BatchInsert1000 | 892 | 1,362 | 1,394 | 1,267 | 995 |
| BatchInsert1000Values | 3,361 | 4,069 | 4,124 | 4,135 | 2,328 |
| TransactionInsert100000 | 9 | 14 | 14 | 13 | 10 |
| SingleRowSelect | 127,033 | 142,035 | 142,329 | 134,652 | 96,915 |
| FullScan1000 | 1,403 | 1,772 | 1,450 | 1,774 | 1,811 |
| IndexedLookup | 119,067 | 135,262 | 138,225 | 130,536 | 87,072 |
| LargeTextRead | 6,424 | 6,348 | 6,417 | 6,894 | 5,942 |
| LargeTextWrite | 12,423 | 12,622 | 12,408 | 12,503 | 9,504 |
| LargeBlobRead | 3,311 | 2,367 | 3,903 | 3,390 | 3,190 |
| LargeBlobWrite | 12,453 | 12,367 | 12,495 | 12,470 | 9,417 |
| WALReaders | 475,520 | 496,454 | 500,155 | 469,406 | 282,068 |
| WALWriterContention | 62,491 | 68,120 | 79,080 | 77,182 | 49,911 |
| FTS5Search | 14,086 | 21,699 | 25,755 | 21,983 | 12,097 |
| JSONQuery | 204,728 | 244,365 | 240,198 | 231,090 | 172,454 |

### Native time / matching-run modernc

Each cell shows the time ratio and matching modernc median in ns/op; lower ratios are better.

| Workload | PureGo | CGO dynamic | CGO static |
|---|---:|---:|---:|
| OpenClose | 0.67x (119,041) | 0.58x (117,241) | 0.57x (114,854) |
| SelectConstant | 1.06x (2,949) | 0.88x (2,924) | 0.89x (2,876) |
| SingleInsert | 0.82x (14,492) | 0.77x (14,233) | 0.76x (14,477) |
| PreparedInsert | 0.83x (11,570) | 0.80x (11,478) | 0.77x (11,647) |
| BatchInsert1000 | 1.11x (1,013,027) | 0.74x (998,110) | 0.71x (1,005,080) |
| BatchInsert1000Values | 0.70x (425,154) | 0.55x (446,667) | 0.57x (428,526) |
| TransactionInsert100000 | 1.11x (100,786,062) | 0.72x (101,287,794) | 0.70x (100,299,874) |
| SingleRowSelect | 0.72x (10,866) | 0.70x (10,095) | 0.68x (10,318) |
| FullScan1000 | 1.21x (590,916) | 1.03x (550,619) | 1.25x (549,717) |
| IndexedLookup | 0.69x (12,105) | 0.64x (11,477) | 0.63x (11,427) |
| LargeTextRead | 0.88x (176,181) | 0.94x (168,299) | 0.96x (162,745) |
| LargeTextWrite | 0.78x (103,810) | 0.75x (105,216) | 0.73x (109,911) |
| LargeBlobRead | 0.96x (313,470) | 1.07x (394,963) | 0.94x (273,479) |
| LargeBlobWrite | 0.76x (105,160) | 0.76x (106,187) | 0.72x (110,523) |
| WALReaders | 0.47x (4,460) | 0.61x (3,294) | 0.56x (3,545) |
| WALWriterContention | 0.58x (27,464) | 0.82x (17,841) | 0.63x (19,971) |
| FTS5Search | 0.86x (82,737) | 0.56x (82,663) | 0.47x (82,413) |
| JSONQuery | 0.83x (5,903) | 0.71x (5,799) | 0.73x (5,713) |

### Matching mattn controls: ns/op

These medians are the denominators for the native backend ratios above.

| Workload | PureGo run | CGO dynamic run | CGO static run |
|---|---:|---:|---:|
| OpenClose | 80,986 | 76,245 | 74,384 |
| SelectConstant | 2,907 | 2,789 | 2,807 |
| SingleInsert | 11,275 | 11,008 | 11,094 |
| PreparedInsert | 8,997 | 8,996 | 8,896 |
| BatchInsert1000 | 852,168 | 789,456 | 748,638 |
| BatchInsert1000Values | 242,210 | 243,414 | 241,768 |
| TransactionInsert100000 | 82,265,091 | 78,963,517 | 73,666,549 |
| SingleRowSelect | 7,679 | 7,381 | 7,415 |
| FullScan1000 | 605,028 | 561,155 | 554,074 |
| IndexedLookup | 8,187 | 7,647 | 7,621 |
| LargeTextRead | 150,318 | 138,784 | 141,880 |
| LargeTextWrite | 79,856 | 79,363 | 80,593 |
| LargeBlobRead | 294,975 | 410,080 | 252,500 |
| LargeBlobWrite | 80,068 | 80,712 | 80,312 |
| WALReaders | 2,206 | 2,124 | 2,087 |
| WALWriterContention | 17,064 | 11,326 | 12,178 |
| FTS5Search | 68,029 | 45,490 | 40,501 |
| JSONQuery | 4,507 | 4,305 | 4,322 |

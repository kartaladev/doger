# Benchmark results: group 54 (design D17)

`bench/baseline.txt` is the code without group 54 (harden-first-release at
1aa41c9, with this branch's `bench/*_test.go`). `bench/after.txt` is this
branch with group 54 and its review fixes (fe72d12, 795e913). Machine: Apple
M4 Pro, darwin/arm64.

## Method

The two sides ran interleaved, 10 rounds of before then after, one sample per
benchmark per round. This way load on the host hits both sides alike. The run
started once the load average was below 2.0 for 1 minute and 2.5 for 5 minutes;
it ended at 2.88. Command per side and round:

    go test -run '^$' -bench . -skip BenchmarkShutdownChain -benchmem -count=1 ./bench
    go test -run '^$' -bench BenchmarkShutdownChain -benchtime=10x -benchmem -count=1 ./bench

`BenchmarkShutdownChain` runs a fixed 10 iterations per sample. Each iteration
first builds a chain of 1000 or 4000 lazy services, untimed. That build costs
about 4 s at 4000, because it is quadratic in the depth of nested provider
calls (unchanged by this group). With the faster shutdown, the default
benchtime would pick about 100 iterations per sample, and the run would take
over an hour.

The baseline first recorded with task 54.1 was replaced by these results.
It was taken on an older base (3936123), and a later non-interleaved "after"
run was taken under host load (load average about 7.5). Those spreads reached
±113%, and benchmarks that allocate less showed up as slower.

## Gate

There is no regression above 5% in sec/op. The largest increases are
`PerRequestScope` (+4.64%) and `New` (+2.67%); B/op of these two rise by
0.95% and 2.20% (the per-scope fields added by group 54).

## benchstat bench/baseline.txt bench/after.txt

```
                             │ baseline │           after           │
                             │       sec/op       │   sec/op     vs base                │
InvokeBuilt/depth_0-14               147.35n ± 2%   99.46n ± 1%  -32.50% (p=0.000 n=10)
InvokeBuilt/depth_5-14                173.3n ± 2%   125.9n ± 0%  -27.35% (p=0.000 n=10)
InvokeBuiltParallel-14                181.5n ± 2%   177.4n ± 1%   -2.26% (p=0.000 n=10)
InvokeAll/unrelated_0-14              2.438µ ± 2%   1.686µ ± 7%  -30.87% (p=0.000 n=10)
InvokeAll/unrelated_1000-14          68.068µ ± 3%   1.836µ ± 4%  -97.30% (p=0.000 n=10)
InvokeAll/unrelated_10000-14        658.923µ ± 2%   1.940µ ± 3%  -99.71% (p=0.000 n=10)
PerRequestScope-14                    14.50µ ± 2%   15.18µ ± 2%   +4.64% (p=0.000 n=10)
ProvideValue100-14                    186.0µ ± 2%   184.5µ ± 2%        ~ (p=0.075 n=10)
HealthCheck50-14                      129.1µ ± 4%   128.5µ ± 3%        ~ (p=0.436 n=10)
InvokeLazySteadyState-14             149.15n ± 3%   98.76n ± 2%  -33.78% (p=0.000 n=10)
InvokeLazyParallel-14                 185.0n ± 5%   177.4n ± 1%   -4.14% (p=0.000 n=10)
InvokeNamed-14                        99.25n ± 2%   84.94n ± 1%  -14.42% (p=0.000 n=10)
InvokeTransient-14                    153.9n ± 2%   110.3n ± 1%  -28.32% (p=0.000 n=10)
InvokeAlias-14                        370.9n ± 2%   343.1n ± 1%   -7.52% (p=0.000 n=10)
InvokeAs-14                           153.7n ± 2%   118.6n ± 1%  -22.84% (p=0.000 n=10)
InvokeAllMixed/10-14                 1622.0n ± 2%   872.5n ± 3%  -46.21% (p=0.000 n=10)
InvokeAllMixed/100-14                16.235µ ± 4%   9.482µ ± 2%  -41.60% (p=0.000 n=10)
InvokeAllMixed/1000-14                219.5µ ± 1%   116.8µ ± 3%  -46.80% (p=0.000 n=10)
InvokeAllParallel-14                 17.881µ ± 2%   6.200µ ± 2%  -65.33% (p=0.000 n=10)
InvokeNestedScopes-14                 177.5n ± 2%   125.4n ± 1%  -29.30% (p=0.000 n=10)
ProviderChainTransient-14            1061.0n ± 3%   864.5n ± 2%  -18.52% (p=0.000 n=10)
Provide-14                            193.9µ ± 4%   188.3µ ± 1%   -2.87% (p=0.000 n=10)
New-14                                779.5n ± 3%   800.4n ± 2%   +2.67% (p=0.009 n=10)
HealthCheck-14                        127.7µ ± 8%   127.7µ ± 5%        ~ (p=0.631 n=10)
Start-14                              21.62µ ± 2%   19.54µ ± 1%   -9.63% (p=0.000 n=10)
Shutdown-14                           81.83µ ± 6%   60.15µ ± 3%  -26.49% (p=0.000 n=10)
ListProvidedServices-14               14.33µ ± 3%   14.45µ ± 2%        ~ (p=0.725 n=10)
Ancestors-14                          20.21n ± 5%   20.17n ± 3%        ~ (p=0.724 n=10)
NewFrameFromCaller-14                 398.3n ± 1%   402.2n ± 2%        ~ (p=0.165 n=10)
NewFrameFromPC-14                     77.52n ± 2%   76.84n ± 3%        ~ (p=0.247 n=10)
ShutdownChain/1000-14                71.276m ± 1%   2.720m ± 2%  -96.18% (p=0.000 n=10)
ShutdownChain/4000-14               1032.07m ± 1%   11.65m ± 3%  -98.87% (p=0.000 n=10)
geomean                               5.733µ        2.764µ       -51.79%

                             │ baseline │              after              │
                             │        B/op        │     B/op      vs base                     │
InvokeBuilt/depth_0-14                 160.0 ± 0%       0.0 ± 0%  -100.00% (p=0.000 n=10)
InvokeBuilt/depth_5-14                 160.0 ± 0%       0.0 ± 0%  -100.00% (p=0.000 n=10)
InvokeBuiltParallel-14                 160.0 ± 0%       0.0 ± 0%  -100.00% (p=0.000 n=10)
InvokeAll/unrelated_0-14             4.563Ki ± 0%   3.484Ki ± 0%   -23.65% (p=0.000 n=10)
InvokeAll/unrelated_1000-14        142.049Ki ± 0%   3.484Ki ± 0%   -97.55% (p=0.000 n=10)
InvokeAll/unrelated_10000-14      1176.943Ki ± 0%   3.485Ki ± 0%   -99.70% (p=0.000 n=10)
PerRequestScope-14                   8.212Ki ± 0%   8.290Ki ± 0%    +0.95% (p=0.000 n=10)
ProvideValue100-14                   167.4Ki ± 0%   158.1Ki ± 0%    -5.56% (p=0.000 n=10)
HealthCheck50-14                     123.7Ki ± 0%   120.5Ki ± 0%    -2.53% (p=0.000 n=10)
InvokeLazySteadyState-14               160.0 ± 0%       0.0 ± 0%  -100.00% (p=0.000 n=10)
InvokeLazyParallel-14                  160.0 ± 0%       0.0 ± 0%  -100.00% (p=0.000 n=10)
InvokeNamed-14                         64.00 ± 0%      0.00 ± 0%  -100.00% (p=0.000 n=10)
InvokeTransient-14                    160.00 ± 0%     64.00 ± 0%   -60.00% (p=0.000 n=10)
InvokeAlias-14                        176.00 ± 0%     64.00 ± 0%   -63.64% (p=0.000 n=10)
InvokeAs-14                            112.0 ± 0%       0.0 ± 0%  -100.00% (p=0.000 n=10)
InvokeAllMixed/10-14                 2.703Ki ± 0%   1.625Ki ± 0%   -39.88% (p=0.000 n=10)
InvokeAllMixed/100-14                26.10Ki ± 0%   15.85Ki ± 0%   -39.27% (p=0.000 n=10)
InvokeAllMixed/1000-14               287.3Ki ± 0%   146.3Ki ± 0%   -49.09% (p=0.000 n=10)
InvokeAllParallel-14                 26.11Ki ± 0%   15.84Ki ± 0%   -39.31% (p=0.000 n=10)
InvokeNestedScopes-14                  160.0 ± 0%       0.0 ± 0%  -100.00% (p=0.000 n=10)
ProviderChainTransient-14             640.00 ± 0%     64.00 ± 0%   -90.00% (p=0.000 n=10)
Provide-14                           165.8Ki ± 0%   156.5Ki ± 0%    -5.62% (p=0.000 n=10)
New-14                               2.836Ki ± 0%   2.898Ki ± 0%    +2.20% (p=0.000 n=10)
HealthCheck-14                       123.6Ki ± 0%   120.5Ki ± 0%    -2.53% (p=0.000 n=10)
Start-14                             31.52Ki ± 0%   26.89Ki ± 0%   -14.68% (p=0.000 n=10)
Shutdown-14                          93.86Ki ± 0%   48.24Ki ± 0%   -48.61% (p=0.000 n=10)
ListProvidedServices-14              53.98Ki ± 0%   53.98Ki ± 0%         ~ (p=1.000 n=10) ¹
Ancestors-14                           80.00 ± 0%     80.00 ± 0%         ~ (p=1.000 n=10) ¹
NewFrameFromCaller-14                  488.0 ± 0%     488.0 ± 0%         ~ (p=1.000 n=10) ¹
NewFrameFromPC-14                      56.00 ± 0%     56.00 ± 0%         ~ (p=1.000 n=10) ¹
ShutdownChain/1000-14              143.622Mi ± 0%   2.627Mi ± 0%   -98.17% (p=0.000 n=10)
ShutdownChain/4000-14              2261.82Mi ± 0%   10.71Mi ± 0%   -99.53% (p=0.000 n=10)
geomean                              6.912Ki                      ?                       ² ³
¹ all samples are equal
² summaries must be >0 to compute geomean
³ ratios must be >0 to compute geomean

                             │ baseline │             after              │
                             │     allocs/op      │  allocs/op   vs base                     │
InvokeBuilt/depth_0-14                 3.000 ± 0%    0.000 ± 0%  -100.00% (p=0.000 n=10)
InvokeBuilt/depth_5-14                 3.000 ± 0%    0.000 ± 0%  -100.00% (p=0.000 n=10)
InvokeBuiltParallel-14                 3.000 ± 0%    0.000 ± 0%  -100.00% (p=0.000 n=10)
InvokeAll/unrelated_0-14               25.00 ± 0%    17.00 ± 0%   -32.00% (p=0.000 n=10)
InvokeAll/unrelated_1000-14            42.00 ± 0%    17.00 ± 0%   -59.52% (p=0.000 n=10)
InvokeAll/unrelated_10000-14          105.00 ± 0%    17.00 ± 0%   -83.81% (p=0.000 n=10)
PerRequestScope-14                     92.00 ± 0%    89.00 ± 0%    -3.26% (p=0.000 n=10)
ProvideValue100-14                    2.240k ± 0%   2.040k ± 0%    -8.93% (p=0.000 n=10)
HealthCheck50-14                      1.372k ± 0%   1.272k ± 0%    -7.29% (p=0.000 n=10)
InvokeLazySteadyState-14               3.000 ± 0%    0.000 ± 0%  -100.00% (p=0.000 n=10)
InvokeLazyParallel-14                  3.000 ± 0%    0.000 ± 0%  -100.00% (p=0.000 n=10)
InvokeNamed-14                         1.000 ± 0%    0.000 ± 0%  -100.00% (p=0.000 n=10)
InvokeTransient-14                     3.000 ± 0%    1.000 ± 0%   -66.67% (p=0.000 n=10)
InvokeAlias-14                         3.000 ± 0%    1.000 ± 0%   -66.67% (p=0.000 n=10)
InvokeAs-14                            2.000 ± 0%    0.000 ± 0%  -100.00% (p=0.000 n=10)
InvokeAllMixed/10-14                   19.00 ± 0%    11.00 ± 0%   -42.11% (p=0.000 n=10)
InvokeAllMixed/100-14                  73.00 ± 0%    59.00 ± 0%   -19.18% (p=0.000 n=10)
InvokeAllMixed/1000-14                 614.0 ± 0%    555.0 ± 0%    -9.61% (p=0.000 n=10)
InvokeAllParallel-14                   73.00 ± 0%    59.00 ± 0%   -19.18% (p=0.000 n=10)
InvokeNestedScopes-14                  3.000 ± 0%    0.000 ± 0%  -100.00% (p=0.000 n=10)
ProviderChainTransient-14             12.000 ± 0%    1.000 ± 0%   -91.67% (p=0.000 n=10)
Provide-14                            2.240k ± 0%   2.040k ± 0%    -8.93% (p=0.000 n=10)
New-14                                 30.00 ± 0%    30.00 ± 0%         ~ (p=1.000 n=10) ¹
HealthCheck-14                        1.372k ± 0%   1.272k ± 0%    -7.29% (p=0.000 n=10)
Start-14                               395.0 ± 0%    295.0 ± 0%   -25.32% (p=0.000 n=10)
Shutdown-14                            409.0 ± 0%    323.0 ± 0%   -21.03% (p=0.000 n=10)
ListProvidedServices-14                73.00 ± 0%    73.00 ± 0%         ~ (p=1.000 n=10) ¹
Ancestors-14                           1.000 ± 0%    1.000 ± 0%         ~ (p=1.000 n=10) ¹
NewFrameFromCaller-14                  7.000 ± 0%    7.000 ± 0%         ~ (p=1.000 n=10) ¹
NewFrameFromPC-14                      3.000 ± 0%    3.000 ± 0%         ~ (p=1.000 n=10) ¹
ShutdownChain/1000-14                 29.84k ± 0%   13.14k ± 0%   -55.97% (p=0.000 n=10)
ShutdownChain/4000-14                164.96k ± 0%   52.30k ± 0%   -68.30% (p=0.000 n=10)
geomean                                43.83                     ?                       ² ³
¹ all samples are equal
² summaries must be >0 to compute geomean
³ ratios must be >0 to compute geomean
```

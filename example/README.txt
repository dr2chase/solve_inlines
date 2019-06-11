Try 

solve_inlines semver.inlines semver.trials

After some delay, you should see something like

# Number of inlines is 21570, trials is 4434, min time is 1162.920000, median time is 1248.980000, avg time is 1250.695877, max time is 1423.580000
Base term = 0.981647
0th percentile[0] = -18.819662
10th percentile[2157] = -0.399962
...
sorted[0] = -18.819662, semver.Constraints.Validate at 72:21 inlines fmt.Errorf, size 461
sorted[1] = -12.368154, runtime.pcvalue at 662:15 inlines runtime.step, size 204
sorted[2] = -1.616468, runtime.gentraceback at 355:26 inlines runtime.funcdata, size 85
...
sorted[21568] = 1.200285, big.(*Accuracy).String at 14:41 inlines strconv.FormatInt, size 113
sorted[21569] = 1.279399, parser.(*parser).parseSwitchStmt at 403:18 inlines parser.(*parser).errorExpected, size 395
sorted[21570] = 2.379323, semver.Constraints.Validate at 23:0 inlines fmt.lastError, size 101
At 1, alleged benefit is -18.819662, last = -18.819662
At 2, alleged benefit is -31.187816, last = -12.368154
At 3, alleged benefit is -32.804284, last = -1.616468
...
At 49, alleged benefit is -77.544260, last = -0.877620
At 50, alleged benefit is -78.420026, last = -0.875767
Number of negative coefficients = 10696, alleged total benefit = -2698.248789


There are issues with the magnitudes of the estimated inlining benefits,
but the first 50 inlining sites tend to contain most of the benefit, at least
when working with inlining decisions above size 80.

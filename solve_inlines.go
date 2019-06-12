// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"github.com/google/gxui/math"
	"gonum.org/v1/gonum/mat"
	"io"
	"math/rand"
	"os"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
)

// inlineRecord describes the relevant parts of a compiler-emitted record of an inlining decision.
type inlineRecord struct {
	callerPackage  string
	callerFunction string
	callerLine     int32
	callerColumn   int32
	inlinePackage  string
	inlineFunction string
	inlineSize     int32
	line           []string // save this for constructed best/worst subsets.
}

// a benchmarkTrial describes a particular build and benchmark run.
// seed and threshold determine which inlining sites are activated and the benchmark time that results.
type benchmarkTrial struct {
	seed      int64
	threshold int32
	time      float64
	noise     int32 // Non-negative if present.  Not used yet.
}

// fillRow initializes a row for least squares solution, given
// a seed and threshold to determine which elements are present.
// The algorithm here is a duplicate of the one in the experimental
// inl.go.
func fillRow(seed int64, threshold int32, row []float64, avg float64) {
	rng := rand.New(rand.NewSource(seed))
	row[len(row)-1] = avg // Constant term
	for i := 0; i < len(row)-1; i++ {
		if rng.Int31n(100) < threshold {
			row[i] = 1
		} else {
			row[i] = 0
		}
	}
}

// readInlines reads a file of inline information used to generate randomized-inlining benchmark results.
func readInlines(fileName string) []inlineRecord {
	f, err := os.Open(fileName)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.Comment = '#'
	var result []inlineRecord
	for {
		line, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(err)
		}
		// Add inlining record
		//                            0                                1         2           3            4          5   6    7              8             9
		// goroots/TEST/src/internal/reflectlite/value.go:440:25: ,front_end,INLINE_SITE,reflectlite,Value.assignTo,440,25,reflectlite,directlyAssignable,217
		callerLine, err := strconv.Atoi(line[5])
		if err != nil {
			panic(err)
		}
		callerColumn, err := strconv.Atoi(line[6])
		if err != nil {
			panic(err)
		}
		inlineSize, err := strconv.Atoi(line[9])
		if err != nil {
			panic(err)
		}
		rec := inlineRecord{callerPackage: line[3], callerFunction: line[4], callerLine: int32(callerLine), callerColumn: int32(callerColumn),
			inlinePackage: line[7], inlineFunction: line[8], inlineSize: int32(inlineSize), line: line}
		result = append(result, rec)
	}
	return result
}

// readBenchmarks parses a file full of benchmark records.
func readBenchmarks(fileName string) []benchmarkTrial {
	f, err := os.Open(fileName)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.Comment = '#'
	var result []benchmarkTrial

	for {
		line, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(err)
		}
		threshold, err := strconv.Atoi(strings.TrimSpace(line[0]))
		if err != nil {
			panic(err)
		}
		seed, err := strconv.Atoi(strings.TrimSpace(line[1]))
		if err != nil {
			panic(err)
		}
		time, err := strconv.ParseFloat(strings.TrimSpace(line[2]), 64)
		if err != nil {
			panic(err)
		}
		noise := -1
		if len(line) >= 4 {
			noise, err = strconv.Atoi(strings.TrimSpace(line[3]))
			if err != nil {
				panic(err)
			}
		}
		rec := benchmarkTrial{seed: int64(seed), threshold: int32(threshold), time: time, noise: int32(noise)}
		result = append(result, rec)
	}
	return result
}

func main() {
	bestN := 0
	worstN := 0
	seed := 0
	threshold := 67
	flag.IntVar(&bestN, "best", bestN, "print the best N inlines in CSV form (best first)")
	flag.IntVar(&worstN, "worst", worstN, "print the worst N inlines in CSV form (worst last)")
	flag.IntVar(&seed, "seed", seed, "Seed for generating a random selection of a file")
	flag.IntVar(&threshold, "threshold", threshold, "Percentage (1-100) of randomly selected lines to include")
	var cpuProfile = flag.String("cpuProfile", "", "write cpu profile to file")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr,
			`
%s [-best N] [-worst N] inlinesFile randomBenchFile  
or
%s -seed N [-threshold N] inlinesFile > inlinesSubsetFile

The first form combines an inlines file with a file containing lines summarizing randomized choice benchmark runs,
where each line has the form "threshold,seed,result" and threshold is a number between 0 and 100 inclusive, to allow
a least-squares estimation of which inlining sites are most important.  -best and -worst modify the output to
be ordered lists of the best (best-first) and worst (worst-last) lines from the point-of-view of improving benchmark
performance.

The second form uses seed and threshold (default 67) to generate an input for a single randomized choice benchmark run.
The subset file is written to standard out.

Because solving for the best/worst inlines can be time-consuming, %s also supports the "-cpuProfile file" option.
`, os.Args[0], os.Args[0])
	}

	flag.Parse()
	args := flag.Args()

	randomBenchFile := ""

	if seed == 0 {
		if len(args) != 2 {
			fmt.Println("two files are required.")
			flag.Usage()
			return
		}
		randomBenchFile = args[1]
	} else if bestN > 0 || worstN > 0 || len(args) != 1 {
		fmt.Println("-seed is incompatible with -bestN, -worstN, and requires a single file argument")
		flag.Usage()
		return
	}
	inlinesFile := args[0]

	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			panic(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	inlines := readInlines(inlinesFile)

	if seed > 0 { // Produce subset, exit.
		vstore := make([]float64, 1+len(inlines))
		// Use fillRow because thus only one copy of the RNG selection.
		fillRow(int64(seed), int32(threshold), vstore, -1)
		csvw := csv.NewWriter(os.Stdout)
		for i, l := range inlines {
			if vstore[i] > 0 {
				csvw.Write(l.line)
			}
		}
		csvw.Flush()
		return
	}

	trials := readBenchmarks(randomBenchFile)

	// Get max, min, median, and average benchmark times.
	sort.Slice(trials, func(i, j int) bool { return trials[i].time < trials[j].time })
	min := trials[0].time
	max := trials[len(trials)-1].time
	median := (trials[(len(trials)-1)/2].time + trials[len(trials)/2].time) / 2

	total := 0.0
	for _, t := range trials {
		total += t.time
	}
	avg := total / float64(len(trials))

	a := mat.NewDense(len(trials), 1+len(inlines), nil)
	vstore := make([]float64, 1+len(inlines))
	v := mat.NewVecDense(1+len(inlines), vstore)
	b := mat.NewVecDense(len(trials), nil)
	for i, t := range trials {
		fillRow(t.seed, t.threshold, a.RawRowView(i), avg)
		b.SetVec(i, float64(t.time))
	}
	fmt.Printf("# Number of inlines is %d, trials is %d, min time is %f, median time is %f, avg time is %f, max time is %f\n", len(inlines), len(trials), min, median, avg, max)

	v.SolveVec(a, b)
	// vstats := append([]float64{}, vstore...)
	sortedOrder := make([]int, len(vstore))
	for i := range sortedOrder {
		sortedOrder[i] = i
	}
	sort.Slice(sortedOrder, func(i, j int) bool {
		return vstore[sortedOrder[i]] < vstore[sortedOrder[j]]
	})

	if bestN > 0 || worstN > 0 {
		if bestN > 0 {
			for i := 0; i < math.Min(bestN, len(sortedOrder)); i++ {
				line := inlines[sortedOrder[i]].line
				fmt.Printf("%s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
					line[0], line[1], line[2], line[3], line[4], line[5], line[6], line[7], line[8], line[9])
			}
			if worstN > 0 {
				fmt.Println() // insert a separating blank line.
			}
		}
		if worstN > 0 {
			// First to check to see if constant term is in worst N.
			l := math.Min(worstN, len(sortedOrder))
			for i := 0; i < l; i++ {
				j := len(sortedOrder) - l + i
				o := sortedOrder[j]
				if o == len(inlines) && l < len(sortedOrder) {
					l++
					break
				}
			}
			for i := 0; i < l; i++ {
				j := len(sortedOrder) - l + i
				o := sortedOrder[j]
				// Check for o = len(inlines)
				if o == len(inlines) {
					continue
				}
				line := inlines[o].line
				fmt.Printf("%s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
					line[0], line[1], line[2], line[3], line[4], line[5], line[6], line[7], line[8], line[9])
			}
		}
		return
	}

	fmt.Printf("Base term = %f\n", vstore[len(vstore)-1])

	for i := 0; i < 11; i++ {
		j := (i*len(sortedOrder) + 5) / 10
		if j == len(sortedOrder) {
			j--
		}
		fmt.Printf("%dth percentile[%d] = %f\n", i*10, j, vstore[sortedOrder[j]])
	}

	for i := 0; i < math.Min(50, len(sortedOrder)); i++ {
		rec := inlines[sortedOrder[i]]
		fmt.Printf("sorted[%d] = %f, %s.%s at %d:%d inlines %s.%s, size %d\n",
			i, vstore[sortedOrder[i]],
			rec.callerPackage, rec.callerFunction, rec.callerLine, rec.callerColumn,
			rec.inlinePackage, rec.inlineFunction, rec.inlineSize)
	}
	fmt.Println()
	for i := math.Min(50, len(sortedOrder)); i > 0; i-- {
		j := len(sortedOrder) - i
		k := sortedOrder[j]
		if k >= len(inlines) {
			// Constant term
			fmt.Printf("sorted[%d] = %f, constant term\n", j, vstore[k])
			continue
		}
		rec := inlines[k]
		fmt.Printf("sorted[%d] = %f, %s.%s at %d:%d inlines %s.%s, size %d\n",
			j, vstore[k],
			rec.callerPackage, rec.callerFunction, rec.callerLine, rec.callerColumn,
			rec.inlinePackage, rec.inlineFunction, rec.inlineSize)
	}

	count := 0
	benefit := 0.0
	printAtBenefit := -1.0
	for k, i := range sortedOrder {
		v := vstore[i]
		if v >= 0 {
			break
		}
		count++
		benefit += v
		if benefit <= printAtBenefit {
			if k < 50 {
				fmt.Printf("At %d, alleged benefit is %f, last = %f\n", count, benefit, v)
			}
			printAtBenefit -= 1
		}
	}
	fmt.Printf("Number of negative coefficients = %d, alleged total benefit = %f\n", count, benefit)

}

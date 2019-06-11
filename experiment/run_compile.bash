#!/bin/bash 

if [ "z${MAXNOISE}" = "z" ] ; then
    MAXNOISE=3
    echo Lacking environment variable MAXNOISE, using default value "${MAXNOISE} (percent)"
fi

# Perflock is preferred
PERFLOCK=`which perflock`
if [ "z${PERFLOCK}" = "z" -a `uname` = "Linux" ] ; then
    echo "You can get cleaner benchmark results on Linux with perflock: go get github.com/aclements/perflock/..."
fi

BENCH=compile

BENCH_INLINES="${BENCH}.inlines"
BENCH_TRIALS="${BENCH}.trials"
BENCH_STAT="${BENCH}.laststat"
BENCH_LOG="${BENCH}.log"

# These ought to be okay.
THRESHOLD=67
COUNT=100000
SEED0=1

# Begin one after the last trial run
if [ -e "${BENCH_TRIALS}" ] ; then
    SEED=`tail -1 "${BENCH_TRIALS}" | awk -F , '{print 0+$2}'`
    SEED0=$((SEED + 1))
    echo "Resuming at seed=${SEED0}"
fi

# Create the record of all the inlines if none exists
if [ ! -e "${BENCH_INLINES}" ] ; then 
   echo "Creating list of all inline sites in ${BENCH_INLINES}"
   mkdir -p testbin
   GO_INLMAXBUDGET=640 GO_INLBIGMAXBUDGET=160 GO_INLRECORDSIZE=20 GO_INLRECORDS=_ go build -a cmd/compile >& compile.inlines.tmp
   grep INLINE_SITE "${BENCH_INLINES}".tmp | sort -u > "${BENCH_INLINES}"
fi

SEEDN=$((SEED0 + COUNT))

# FYI `eval echo {${SEED0}..${SEEDN}}` does what you want.
for S in `eval echo {${SEED0}..${SEEDN}}` ; do 
# expects environment variables T and S to be set
    rm -rf goroots testbin
    mkdir -p testbin
    solve_inlines -seed ${S} -threshold ${THRESHOLD} "${BENCH_INLINES}" > inlines.txt
	go clean -cache
	GO_INLMAXBUDGET=640 GO_INLBIGMAXBUDGET=160 GO_INLRECORDSIZE=20 GO_INLRECORDS=$PWD/inlines.txt go build -a cmd/compile 
	while \
  		GOMAXPROCS=4 $PERFLOCK compilebench -compile ${PWD}/compile -count 25 -run BenchmarkCompile | sed -E -e 's?[0-9]+ ns/op ??' > testbin/compile.TEST.stdout

        benchstat -geomean -csv testbin/*.TEST.stdout >& "${BENCH_STAT}"

        tail -1 "${BENCH_STAT}" >> "${BENCH_LOG}"
        # Noise depends on whether it is one benchmark or several
        if grep -q "Geo mean" "${BENCH_STAT}" ; then
                # Extract max noise from all the benchmarks.
                # awk: using comma as field separator, match 3-field lines where last field ends in "%", compute maxnoise, print it at the end.
                NOISE=`awk -F , 'NF == 3 && $3~/.*%/ { gsub(" ","",$3); noise = 0+$3; if (noise > maxnoise) maxnoise=noise;} END {print maxnoise;}' < "${BENCH_STAT}" `
        else
                NOISE=`tail -1 "${BENCH_STAT}" | awk -F , '{gsub(" ","",$3); print $3}' | sed -e '1,$s/%//'`
        fi
        TIME=`tail -1 "${BENCH_STAT}" | awk -F , '{gsub(" ","",$2); print $2}' ` 
        echo "Seed=${S}, Threshold=${THRESHOLD}, Time=${TIME}, Max noise=${NOISE}"
        if test -z "${NOISE}" ; then
                true
        else
                test ${NOISE} -gt ${MAXNOISE}
        fi
	do
        echo "Too noisy (${NOISE}), repeating test"
	done 

echo "${THRESHOLD},${S},${TIME},${NOISE}" >> "${BENCH_TRIALS}"
done

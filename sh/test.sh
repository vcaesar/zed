#!/bin/bash

cd "$(dirname "$0")/.." || exit 1

export ZED_FIRST_ADMIN_USER=admin  
export ZED_FIRST_ADMIN_PASSWORD=Complexpass#123
export ZED_WAL_SYNC_INTERVAL=10ms
export ZED_WAL_REDOLOG_NO_SYNC=true
export ZED_ENABLE_TEXT_KEYWORD_MAPPING=true

find ./pkg -name data -type dir|xargs rm -fR
find ./test -name data -type dir|xargs rm -fR

if [[ $1 == "bench" ]]; then
    go test -v -test.run=NONE -test.bench=Bulk -benchmem ./test/benchmark/ -cpuprofile=./tmp/cpu.pprof -memprofile=./tmp/mem.pprof
    # go tool pprof -http=:9999 ./tmp/mem.pprof
else
    go test -v ./... -test.run=$1
    # release build tag (no embedded gse dict), see sh/build.sh
    go test -v -tags ne ./pkg/bluge/analysis/lang/chs/... -test.run=$1
fi

find ./pkg -name data -type dir|xargs rm -fR
find ./test -name data -type dir|xargs rm -fR

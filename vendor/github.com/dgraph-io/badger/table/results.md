#### Sequential Reads(BenchmarkRead)
For 5M keys, Sequential reads are **~20% slower** in the new format.
```
go test -bench ^BenchmarkRead$ -run ^$ -benchtime 10s > master
goos: linux
goarch: amd64
pkg: github.com/dgraph-io/badger/table
BenchmarkRead-16    	     100	 168164560 ns/op
PASS
ok  	github.com/dgraph-io/badger/table	23.186s
```
```
goos: linux
goarch: amd64
pkg: github.com/dgraph-io/badger/table
BenchmarkRead-16    	     100	 202280932 ns/op
--- BENCH: BenchmarkRead-16
    table_test.go:652: Size of table: 128714148
    table_test.go:652: Size of table: 128714148
PASS
ok  	github.com/dgraph-io/badger/table	26.379s
```
```
benchcmp master ashish
benchmark            old ns/op     new ns/op     delta
BenchmarkRead-16     168164560     202280932     +20.29%
```

#### Random Reads(BenchmarkReadRandom)
For 5M keys, random reads are **~18% faster** in the new format.
```
go test -bench ^BenchmarkReadRandom$ -run ^$ -benchtime 10s > master_10s
goos: linux
goarch: amd64
pkg: github.com/dgraph-io/badger/table
BenchmarkReadRandom-16    	 5000000	      3928 ns/op
PASS
ok  	github.com/dgraph-io/badger/table	121.876s
```
```
goos: linux
goarch: amd64
pkg: github.com/dgraph-io/badger/table
BenchmarkReadRandom-16    	 5000000	      3197 ns/op
PASS
ok  	github.com/dgraph-io/badger/table	134.863s
```
```
benchcmp master_10s ashish_10s 
benchmark                  old ns/op     new ns/op     delta
BenchmarkReadRandom-16     3928          3197          -18.61%
```
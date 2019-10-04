Benchmark KeyItertator on p directory
```go
package badger

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/dgraph-io/badger/pb"
	"github.com/stretchr/testify/require"
)

var sampleSize = 50000000 // 50 million

func BenchmarkSeek(b *testing.B) {
	opts := DefaultOptions
	opts.Dir = "/mnt/m2/p"
	opts.ValueDir = "/mnt/m2/p"
	opts.ReadOnly = true

	db, err := Open(opts)
	require.NoError(b, err)
	defer db.Close()

	txn := db.NewTransaction(false)
	defer txn.Discard()

	op := DefaultIteratorOptions

	keys, err := getSampleKeys(db)
	require.NoError(b, err)

	fmt.Println("Total Sampled Keys: ", len(keys))
	r := rand.New(rand.NewSource(time.Now().Unix()))

	b.Run("BenchSeek", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			key := keys[r.Intn(len(keys))]
			itr := txn.NewKeyIterator(key, op)
			itr.Rewind()
			itm := itr.Item()
			require.Equal(b, key, itm.Key())
			for ; itr.Valid(); itr.Next() {
			}
			itr.Close()
		}
	})
}

func getSampleKeys(db *DB) ([][]byte, error) {
	var keys [][]byte
	count := 0
	stream := db.NewStream()

	stream.KeyToList = func(key []byte, itr *Iterator) (*pb.KVList, error) {
		list := &pb.KVList{}
		// Since stream framework copies the item's key while calling
		// KeyToList, we can directly append key to list.
		list.Kv = append(list.Kv, &pb.KV{Key: key})
		return list, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream.Send = func(list *pb.KVList) error {
		if count >= sampleSize {
			return nil
		}
		for _, kv := range list.Kv {
			keys = append(keys, kv.Key)
			count++
			if count >= sampleSize {
				cancel()
				break
			}
		}
		return nil
	}

	if err := stream.Orchestrate(ctx); err != nil && err != context.Canceled {
		return nil, err
	}

	rand.Seed(time.Now().Unix())
	rand.Shuffle(len(keys), func(i, j int) {
		keys[i], keys[j] = keys[j], keys[i]
	})

	return keys, nil
}
```
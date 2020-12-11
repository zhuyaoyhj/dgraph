/*
 * Copyright 2017-2018 Dgraph Labs, Inc. and Contributors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package bulk

import (
	"fmt"
	"github.com/dgraph-io/dgraph/types"
	"log"
	"math"
	"strings"
	"sync"

	"github.com/dgraph-io/badger/v2"
	"github.com/dgraph-io/dgraph/posting"
	"github.com/dgraph-io/dgraph/protos/pb"
	"github.com/dgraph-io/dgraph/schema"
	wk "github.com/dgraph-io/dgraph/worker"
	"github.com/dgraph-io/dgraph/x"
)

type schemaStore struct {
	sync.RWMutex
	schemaMap map[string]*pb.SchemaUpdate
	types     []*pb.TypeUpdate
	*state
}

func newSchemaStore(initial *schema.ParsedSchema, opt *options, state *state) *schemaStore {
	if opt == nil {
		log.Fatalf("Cannot create schema store with nil options.")
	}

	s := &schemaStore{
		schemaMap: map[string]*pb.SchemaUpdate{},
		state:     state,
	}

	// Load all initial predicates. Some predicates that might not be used when
	// the alpha is started (e.g ACL predicates) might be included but it's
	// better to include them in case the input data contains triples with these
	// predicates.
	for _, update := range schema.CompleteInitialSchema() {
		s.schemaMap[update.Predicate] = update
	}

	if opt.StoreXids {
		s.schemaMap["xid"] = &pb.SchemaUpdate{
			ValueType: pb.Posting_STRING,
			Tokenizer: []string{"hash"},
		}
	}

	for _, sch := range initial.Preds {
		p := sch.Predicate
		sch.Predicate = "" // Predicate is stored in the (badger) key, so not needed in the value.
		if _, ok := s.schemaMap[p]; ok {
			fmt.Printf("Predicate %q already exists in schema\n", p)
			continue
		}
		s.schemaMap[p] = sch
	}

	s.types = initial.Types

	return s
}

func (s *schemaStore) getSchema(pred string) *pb.SchemaUpdate {
	s.RLock()
	defer s.RUnlock()
	return s.schemaMap[pred]
}

func (s *schemaStore) setSchemaAsList(pred string) {
	s.Lock()
	defer s.Unlock()
	sch, ok := s.schemaMap[pred]
	if !ok {
		return
	}
	sch.List = true
}

//return value of function validate for remove removeIncosistentData
const (
	Datatypeincosistent = "data and schema type incosistent"
	ValidateTypesuccess = "type correct"
)

func (s *schemaStore) validateType(de *pb.DirectedEdge, objectIsUID bool, AppendLangTags bool, RemoveInconsistentData bool) string {
	if objectIsUID {
		de.ValueType = pb.Posting_UID
	}

	s.RLock()
	sch, ok := s.schemaMap[de.Attr]
	s.RUnlock()
	if !ok {
		s.Lock()
		sch, ok = s.schemaMap[de.Attr]
		if !ok {
			sch = &pb.SchemaUpdate{ValueType: de.ValueType}
			if objectIsUID {
				sch.List = true
			}
			s.schemaMap[de.Attr] = sch
		}
		s.Unlock()
	}

	//yhj-code
	//err := wk.ValidateAndConvert(de, sch)
	var err error
	if AppendLangTags {
		err = wk.ValidateAndConvertAppendLangTags(de, sch)
	} else {
		err = wk.ValidateAndConvert(de, sch)
	}
	if err != nil {
		//yhj-code
		if RemoveInconsistentData {
			if strings.Contains(err.Error(), "Input for predicate") && strings.Contains(err.Error(), "of type uid is scalar") {
				fmt.Printf("RemoveInconsistentData! inconsistent type between rdf data and schema. err = %v \n", err)
				return Datatypeincosistent
			} else if strings.Contains(err.Error(), "Input for predicate") && strings.Contains(err.Error(), "of type scalar is uid. Edge") {
				fmt.Printf("RemoveInconsistentData! inconsistent type between rdf data and schema. err = %v \n", err)
				return Datatypeincosistent
			} else {
				fmt.Printf("RDF doesn't match schema: %v, edge info: %v, edge type: %v, schema type: %v", err, de, posting.TypeID(de), types.TypeID(sch.ValueType))
				//log.Fatalf("RDF doesn't match schema: %v, edge info: %v, edge type: %v, schema type: %v", err, de, posting.TypeID(de), types.TypeID(sch.ValueType))
			}
		} else {
			fmt.Printf("RDF doesn't match schema: %v", err)
			return Datatypeincosistent
		}
		//log.Fatalf("RDF doesn't match schema: %v", err)
	}
	return ValidateTypesuccess
	//yhj-code end
}

func (s *schemaStore) getPredicates(db *badger.DB) []string {
	txn := db.NewTransactionAt(math.MaxUint64, false)
	defer txn.Discard()

	opts := badger.DefaultIteratorOptions
	opts.PrefetchValues = false
	itr := txn.NewIterator(opts)
	defer itr.Close()

	m := make(map[string]struct{})
	for itr.Rewind(); itr.Valid(); {
		item := itr.Item()
		pk, err := x.Parse(item.Key())
		x.Check(err)
		m[pk.Attr] = struct{}{}
		itr.Seek(pk.SkipPredicate())
		continue
	}

	var preds []string
	for pred := range m {
		preds = append(preds, pred)
	}
	return preds
}

func (s *schemaStore) write(db *badger.DB, preds []string) {
	w := posting.NewTxnWriter(db)
	for _, pred := range preds {
		sch, ok := s.schemaMap[pred]
		if !ok {
			continue
		}
		k := x.SchemaKey(pred)
		v, err := sch.Marshal()
		x.Check(err)
		// Write schema and types always at timestamp 1, s.state.writeTs may not be equal to 1
		// if bulk loader was restarted or other similar scenarios.
		x.Check(w.SetAt(k, v, posting.BitSchemaPosting, 1))
	}

	//yhj-code create schemaorg:Thing type
	var typsTemp []*pb.TypeUpdate
	for _, typ := range s.types {
		var thingSystem = &pb.TypeUpdate{
			TypeName: typ.TypeName,
		}
		var typePredMap = make(map[string]struct{})

		for _, pred := range typ.Fields {
			//只加入schemaorg和kgsogou的属性
			if strings.Contains(pred.Predicate, "dgraph") || !strings.Contains(pred.Predicate, "schemaorg") || !strings.Contains(pred.Predicate, "kgsogou") {
				continue
			}
			if _, ok := typePredMap[pred.Predicate]; ok {
				continue
			}
			schema := &pb.SchemaUpdate{Predicate: pred.Predicate}
			thingSystem.Fields = append(thingSystem.Fields, schema)
			typePredMap[pred.Predicate] = struct{}{}
		}

		for pred, _ := range s.schemaMap {
			if strings.Contains(pred, "dgraph") || !strings.Contains(pred, "schemaorg") || !strings.Contains(pred, "kgsogou") {
				continue
			}
			//if updateSchema.Directive == pb.SchemaUpdate_REVERSE {
			//	predd := "~" + pred
			//	schema := &pb.SchemaUpdate{Predicate: predd}
			//	thingSystem.Fields = append(thingSystem.Fields, schema)
			//	typePredMap[predd] = struct{}{}
			//}
			if _, ok := typePredMap[pred]; ok {
				continue
			}
			schema := &pb.SchemaUpdate{Predicate: pred}
			thingSystem.Fields = append(thingSystem.Fields, schema)
			typePredMap[pred] = struct{}{}
		}
		typsTemp = append(typsTemp, thingSystem)
	}
	//fmt.Println("origin")
	//for _, v := range s.types {
	//	fmt.Println(v.TypeName)
	//	for _, vf := range v.Fields {
	//		temp, err := vf.Marshal()
	//		fmt.Println(string(temp), err)
	//	}
	//}

	s.types = typsTemp
	//fmt.Println("new")
	//for _, v := range s.types {
	//	fmt.Println(v.TypeName)
	//	for _, vf := range v.Fields {
	//		temp, err := vf.Marshal()
	//		fmt.Println(string(temp), err)
	//	}
	//}
	//s.types = append(s.types, typsTemp...)
	//yhj-code end

	// Write all the types as all groups should have access to all the types.
	for _, typ := range s.types {
		k := x.TypeKey(typ.TypeName)
		v, err := typ.Marshal()
		x.Check(err)
		x.Check(w.SetAt(k, v, posting.BitSchemaPosting, 1))
	}

	x.Check(w.Flush())
}

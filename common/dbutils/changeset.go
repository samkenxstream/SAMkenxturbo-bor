package dbutils

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/ledgerwatch/turbo-geth/common"
)

type Change struct {
	Key   []byte
	Value []byte
}

// ChangeSet is a map with keys of the same size.
// Both keys and values are byte strings.
type ChangeSet struct {
	// Invariant: all keys are of the same size.
	Changes []Change
}

// BEGIN sort.Interface

func (s *ChangeSet) Len() int {
	return len(s.Changes)
}

func (s *ChangeSet) Swap(i, j int) {
	s.Changes[i], s.Changes[j] = s.Changes[j], s.Changes[i]
}

func (s *ChangeSet) Less(i, j int) bool {
	cmp := bytes.Compare(s.Changes[i].Key, s.Changes[j].Key)
	return cmp < 0
}

// END sort.Interface

/*
ChangeSet is serialized in the following manner in order to facilitate binary search:
1. The number of keys N (uint32, 4 bytes).
2. The key size M (uint32, 4 bytes).
3. Contiguous array of keys (N*M bytes).
4. Contiguous array of accumulating value indexes:
len(val0), len(val0)+len(val1), ..., len(val0)+len(val1)+...+len(val_{N-1})
(4*N bytes since the lengths are treated as uint32).
5. Contiguous array of values.

uint32 integers are serialized as big-endian.
*/

// Encode sorts a ChangeSet by key and then serializes it.
func (s *ChangeSet) Encode() ([]byte, error) {
	sort.Sort(s)

	buf := new(bytes.Buffer)
	intArr := make([]byte, 4)
	n := s.Len()
	binary.BigEndian.PutUint32(intArr, uint32(n))
	_, err := buf.Write(intArr)
	if err != nil {
		return nil, err
	}

	m := s.KeySize()
	binary.BigEndian.PutUint32(intArr, uint32(m))
	_, err = buf.Write(intArr)
	if err != nil {
		return nil, err
	}

	for i := 0; i < n; i++ {
		_, err = buf.Write(s.Changes[i].Key)
		if err != nil {
			return nil, err
		}
	}

	var l int
	for i := 0; i < n; i++ {
		l += len(s.Changes[i].Value)
		binary.BigEndian.PutUint32(intArr, uint32(l))
		_, err = buf.Write(intArr)
		if err != nil {
			return nil, err
		}
	}

	for i := 0; i < n; i++ {
		_, err = buf.Write(s.Changes[i].Value)
		if err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

func Decode(b []byte) (ChangeSet, error) {
	h := ChangeSet{}
	h.Changes = make([]Change, 0)
	if len(b) == 0 {
		return h, nil
	}

	if len(b) < 8 {
		return h, fmt.Errorf("decode: input too short (%d bytes)", len(b))
	}

	n := binary.BigEndian.Uint32(b[0:4])
	m := binary.BigEndian.Uint32(b[4:8])

	if n == 0 {
		return h, nil
	}

	h.Changes = make([]Change, n)

	valOffset := 8 + n*m + 4*n
	if uint32(len(b)) < valOffset {
		return h, fmt.Errorf("decode: input too short (%d bytes, expected at least %d bytes)", len(b), valOffset)
	}

	totalValLength := binary.BigEndian.Uint32(b[valOffset-4 : valOffset])
	if uint32(len(b)) < valOffset+totalValLength {
		return h, fmt.Errorf("decode: input too short (%d bytes, expected at least %d bytes)", len(b), valOffset+totalValLength)
	}

	for i := uint32(0); i < n; i++ {
		key := b[8+i*m : 8+(i+1)*m]
		idx0 := uint32(0)
		if i > 0 {
			idx0 = binary.BigEndian.Uint32(b[8+n*m+4*(i-1) : 8+n*m+4*i])
		}
		idx1 := binary.BigEndian.Uint32(b[8+n*m+4*i : 8+n*m+4*(i+1)])
		val := b[valOffset+idx0 : valOffset+idx1]

		h.Changes[i].Key = common.CopyBytes(key)
		h.Changes[i].Value = common.CopyBytes(val)
	}

	return h, nil
}

func (s *ChangeSet) KeySize() int {
	for _, c := range s.Changes {
		return len(c.Key)
	}
	return 0
}

func (s *ChangeSet) checkKeySize(key []byte) error {
	if s.Len() == 0 || len(key) == s.KeySize() {
		return nil
	}

	return fmt.Errorf("wrong key size in ChangeSet: expected %d, actual %d", s.KeySize(), len(key))
}

// Add adds a new entry to the ChangeSet.
// One must not add an existing key
// and may add keys only of the same size.
func (s *ChangeSet) Add(key []byte, value []byte) error {
	if err := s.checkKeySize(key); err != nil {
		return err
	}

	s.Changes = append(s.Changes, Change{
		Key:   key,
		Value: value,
	})

	return nil
}

// MultiAdd adds multiple new entries to the ChangeSet.
// One must not add existing keys
// and may add keys only of the same size.
func (s *ChangeSet) MultiAdd(changes []Change) error {
	for i := range changes {
		if err := s.checkKeySize(changes[i].Key); err != nil {
			return err
		}
		s.Changes = append(s.Changes, changes[i])
	}

	return nil
}

func (s *ChangeSet) Walk(f func(k, v []byte) error) error {
	for i := range s.Changes {
		err := f(s.Changes[i].Key, s.Changes[i].Value)
		if err != nil {
			return err
		}
	}
	return nil
}
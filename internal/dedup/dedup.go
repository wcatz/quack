package dedup

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	seenBucket   = []byte("seen")
	indexBucket  = []byte("index")
	countsBucket = []byte("counts")

	imagesKey = []byte("images")
	gifsKey   = []byte("gifs")
)

type Store struct {
	db     *bolt.DB
	logger *slog.Logger
	mu     sync.RWMutex
}

func Open(path string, logger *slog.Logger) (*Store, error) {
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open dedup db: %w", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{seenBucket, indexBucket, countsBucket} {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("init dedup buckets: %w", err)
	}

	return &Store{db: db, logger: logger}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// Seen returns true if the source+id pair has already been processed.
func (s *Store) Seen(source, id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := source + ":" + id
	var found bool
	s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(seenBucket)
		found = b.Get([]byte(key)) != nil
		return nil
	})
	return found
}

// Mark records a source+id as processed and maps it to the object key.
func (s *Store) Mark(source, id, objectKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	seenKey := source + ":" + id
	ts := time.Now().UTC().Format(time.RFC3339)

	return s.db.Update(func(tx *bolt.Tx) error {
		seen := tx.Bucket(seenBucket)
		if err := seen.Put([]byte(seenKey), []byte(objectKey+"|"+ts)); err != nil {
			return err
		}

		idx := tx.Bucket(indexBucket)
		if err := idx.Put([]byte(objectKey), []byte(source+"|"+id)); err != nil {
			return err
		}

		return nil
	})
}

// IncrementCount increments the count for the given type ("images" or "gifs").
func (s *Store) IncrementCount(mediaType string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := imagesKey
	if mediaType == "gifs" {
		key = gifsKey
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(countsBucket)
		val := b.Get(key)
		var count uint64
		if val != nil {
			count = binary.BigEndian.Uint64(val)
		}
		count++
		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, count)
		return b.Put(key, buf)
	})
}

// Counts returns the number of images and gifs.
func (s *Store) Counts() (images, gifs int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(countsBucket)
		if val := b.Get(imagesKey); val != nil {
			images = int(binary.BigEndian.Uint64(val))
		}
		if val := b.Get(gifsKey); val != nil {
			gifs = int(binary.BigEndian.Uint64(val))
		}
		return nil
	})
	return
}

// AllKeys returns all object keys stored in the index.
func (s *Store) AllKeys() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var keys []string
	s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(indexBucket)
		return b.ForEach(func(k, _ []byte) error {
			keys = append(keys, string(k))
			return nil
		})
	})
	return keys
}

package myethdb

import (
	"fmt"
	"github.com/cockroachdb/pebble"
	"github.com/cockroachdb/pebble/bloom"
	gometrics "github.com/rcrowley/go-metrics"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

import (
	"awesomeProject1/goleveldb/leveldb"
	"awesomeProject1/goleveldb/leveldb/errors"
	"awesomeProject1/goleveldb/leveldb/filter"
	"awesomeProject1/goleveldb/leveldb/iterator"
	"awesomeProject1/goleveldb/leveldb/opt"
	"github.com/ethereum/go-ethereum/log"
)

type LDBDatabase struct {
	fn   string      // filename for reporting
	db   *leveldb.DB // LevelDB instance
	db_p *pebble.DB  // LevelDB instance

	getTimer       gometrics.Timer // Timer for measuring the database get request counts and latencies
	putTimer       gometrics.Timer // Timer for measuring the database put request counts and latencies
	delTimer       gometrics.Timer // Timer for measuring the database delete request counts and latencies
	missMeter      gometrics.Meter // Meter for measuring the missed database get requests
	readMeter      gometrics.Meter // Meter for measuring the database get request data usage
	writeMeter     gometrics.Meter // Meter for measuring the database put request data usage
	compTimeMeter  gometrics.Meter // Meter for measuring the total time spent in database compaction
	compReadMeter  gometrics.Meter // Meter for measuring the data read during compaction
	compWriteMeter gometrics.Meter // Meter for measuring the data written during compaction

	quitLock sync.Mutex      // Mutex protecting the quit channel access
	quitChan chan chan error // Quit channel to stop the metrics collection before closing the database

	log log.Logger // Contextual logger tracking the database path
}

func NewLDBDatabase(file string, cache int, handles int) (*LDBDatabase, error) {

	// Ensure we have some minimal caching and file guarantees
	if cache < 16 {
		cache = 16
	}
	//if handles < 16 {
	//	handles = 16
	//}

	// Open the db and recover any potential corruptions
	db, err := leveldb.OpenFile(file, &opt.Options{
		OpenFilesCacheCapacity: handles,
		BlockCacheCapacity:     cache / 2 * opt.MiB,
		WriteBuffer:            cache / 4 * opt.MiB, // Two of these are used internally
		Filter:                 filter.NewBloomFilter(10),
		Compression:            opt.NoCompression,
		//ReadOnly:true,
		DisableBlockCache: true,
	})
	if _, corrupted := err.(*errors.ErrCorrupted); corrupted {
		db, err = leveldb.RecoverFile(file, nil)
	}
	// (Re)check for errors and abort if opening of the db failed
	if err != nil {
		return nil, err
	}

	fmt.Println("-------------------------------------------open success")
	return &LDBDatabase{
		fn: file, // 文件名
		db: db,   // 数据库对象
	}, nil
}

func NewLDBDatabase_pebble(file string, cache int, handles int) (*LDBDatabase, error) {

	// Ensure we have some minimal caching and file guarantees
	if cache < 8 {
		cache = 8
	}
	//if handles < 16 {
	//	handles = 16
	//}

	maxMemTableSize := (1<<31)<<(^uint(0)>>63) - 1
	// Two memory tables is configured which is identical to leveldb,
	// including a frozen memory table and another live one.
	memTableLimit := 2
	memTableSize := cache * 1024 * 1024 / 2 / memTableLimit

	// The memory table size is currently capped at maxMemTableSize-1 due to a
	// known bug in the pebble where maxMemTableSize is not recognized as a
	// valid size.
	//
	// TODO use the maxMemTableSize as the maximum table size once the issue
	// in pebble is fixed.
	if memTableSize >= maxMemTableSize {
		memTableSize = maxMemTableSize - 1
	}
	opt := &pebble.Options{
		// Pebble has a single combined cache area and the write
		// buffers are taken from this too. Assign all available
		// memory allowance for cache.
		Cache:        pebble.NewCache(int64(cache * 1024 * 1024)),
		MaxOpenFiles: handles,

		// The size of memory table(as well as the write buffer).
		// Note, there may have more than two memory tables in the system.
		MemTableSize: uint64(memTableSize),

		// MemTableStopWritesThreshold places a hard limit on the size
		// of the existent MemTables(including the frozen one).
		// Note, this must be the number of tables not the size of all memtables
		// according to https://github.com/cockroachdb/pebble/blob/master/options.go#L738-L742
		// and to https://github.com/cockroachdb/pebble/blob/master/db.go#L1892-L1903.
		MemTableStopWritesThreshold: memTableLimit,

		// The default compaction concurrency(1 thread),
		// Here use all available CPUs for faster compaction.
		MaxConcurrentCompactions: runtime.NumCPU,

		// Per-level options. Options for at least one level must be specified. The
		// options for the last level are used for all subsequent levels.
		Levels: []pebble.LevelOptions{
			{TargetFileSize: 2 * 1024 * 1024, FilterPolicy: bloom.FilterPolicy(10)},
			{TargetFileSize: 4 * 1024 * 1024, FilterPolicy: bloom.FilterPolicy(10)},
			{TargetFileSize: 8 * 1024 * 1024, FilterPolicy: bloom.FilterPolicy(10)},
			{TargetFileSize: 16 * 1024 * 1024, FilterPolicy: bloom.FilterPolicy(10)},
			{TargetFileSize: 32 * 1024 * 1024, FilterPolicy: bloom.FilterPolicy(10)},
			{TargetFileSize: 64 * 1024 * 1024, FilterPolicy: bloom.FilterPolicy(10)},
			{TargetFileSize: 128 * 1024 * 1024, FilterPolicy: bloom.FilterPolicy(10)},
		},
		ReadOnly: false,
	}
	// Disable seek compaction explicitly. Check https://github.com/ethereum/go-ethereum/pull/20130
	// for more details.
	opt.Experimental.ReadSamplingMultiplier = -1

	// Open the db and recover any potential corruptions
	db, err := pebble.Open("./demo", opt)
	if err != nil {
	}

	// (Re)check for errors and abort if opening of the db failed
	if err != nil {
		return nil, err
	}

	fmt.Println("-------------------------------------------open success")
	return &LDBDatabase{
		fn:   file, // 文件名
		db:   nil,  // 数据库对象
		db_p: db,
	}, nil
}

func NewLDBDatabase2(file string, cache int, handles int) (*LDBDatabase, error) {

	// Ensure we have some minimal caching and file guarantees
	if cache < 16 {
		cache = 16
	}
	if handles < 16 {
		handles = 16
	}

	// Open the db and recover any potential corruptions
	db, err := leveldb.OpenFile(file, &opt.Options{
		OpenFilesCacheCapacity: handles / 2,
		BlockCacheCapacity:     cache / 4 * opt.MiB,
		WriteBuffer:            cache / 4 * opt.MiB, // Two of these are used internally
		Filter:                 filter.NewBloomFilter(10),
		Compression:            opt.NoCompression,
		//ReadOnly:true,
		//DisableBlockCache:true,
	})
	if _, corrupted := err.(*errors.ErrCorrupted); corrupted {
		db, err = leveldb.RecoverFile(file, nil)
	}
	// (Re)check for errors and abort if opening of the db failed
	if err != nil {
		return nil, err
	}
	return &LDBDatabase{
		fn: file, // 文件名
		db: db,   // 数据库对象
	}, nil
}

func (db *LDBDatabase) Path() string {
	return db.fn
}

var (
	I    int
	Size int
)

func (db *LDBDatabase) Put(key []byte, value []byte) error {
	// Measure the database put latency, if requested
	if db.putTimer != nil {
		defer db.putTimer.UpdateSince(time.Now())
	}
	// Generate the data to write to disk, update the meter and write
	//value = rle.Compress(value)

	if db.writeMeter != nil {
		db.writeMeter.Mark(int64(len(value)))
	}
	//fmt.Println(i)
	I++
	Size += len(key) + len(value)
	//fmt.Println("From DB",key)
	if db.db != nil {
		return db.db.Put(key, value, nil)
	}
	return db.db_p.Set(key, value, nil)
}

func (db *LDBDatabase) Put_s(key []byte, value []byte) error {
	// Measure the database put latency, if requested
	if db.putTimer != nil {
		defer db.putTimer.UpdateSince(time.Now())
	}
	// Generate the data to write to disk, update the meter and write
	//value = rle.Compress(value)

	if db.writeMeter != nil {
		db.writeMeter.Mark(int64(len(value)))
	}
	//fmt.Println(i)
	I++
	Size += len(key) + len(value)
	return db.db.Put_s(key, value, nil)
}

func (db *LDBDatabase) Has(key []byte) (bool, error) {
	return db.db.Has(key, nil)
}

// Get returns the given key if it's present.
var (
	Ti    time.Time
	Tj    time.Time
	T     float64
	Count int
)

func (db *LDBDatabase) Get(key []byte) ([]byte, error) {
	// Measure the database get latency, if requested
	if db.getTimer != nil {
		defer db.getTimer.UpdateSince(time.Now())
	}
	// Retrieve the key and increment the miss counter if not found
	Ti = time.Now()
	var dat []byte
	var err error
	if db.db != nil {
		dat, err = db.db.Get(key, nil)
	} else {
		dat, _, err = db.db_p.Get(key)
	}

	Tj = time.Now()
	Count++
	T += Tj.Sub(Ti).Seconds()
	if err != nil {
		if db.missMeter != nil {
			db.missMeter.Mark(1)
		}
		return nil, err
	}
	// Otherwise update the actually retrieved amount of data
	if db.readMeter != nil {
		db.readMeter.Mark(int64(len(dat)))
	}
	return dat, nil
	//return rle.Decompress(dat)
}
func (db *LDBDatabase) Get_s(key []byte) ([]byte, error) {
	// Measure the database get latency, if requested
	if db.getTimer != nil {
		defer db.getTimer.UpdateSince(time.Now())
	}
	// Retrieve the key and increment the miss counter if not found
	Ti = time.Now()
	dat, err := db.db.Get_s(key, nil)
	Tj = time.Now()
	Count++
	T += Tj.Sub(Ti).Seconds()
	if err != nil {
		if db.missMeter != nil {
			db.missMeter.Mark(1)
		}
		return nil, err
	}
	// Otherwise update the actually retrieved amount of data
	if db.readMeter != nil {
		db.readMeter.Mark(int64(len(dat)))
	}
	return dat, nil
	//return rle.Decompress(dat)
}

// Delete deletes the key from the queue and database
func (db *LDBDatabase) Delete(key []byte) error {
	// Measure the database delete latency, if requested
	if db.delTimer != nil {
		defer db.delTimer.UpdateSince(time.Now())
	}
	// Execute the actual operation
	return db.db.Delete(key, nil)
}

func (db *LDBDatabase) NewIterator() iterator.Iterator {
	return db.db.NewIterator(nil, nil)
}

func (db *LDBDatabase) Close() {
	// Stop the metrics collection to avoid internal database races
	db.quitLock.Lock()
	defer db.quitLock.Unlock()

	if db.quitChan != nil {
		errc := make(chan error)
		db.quitChan <- errc
		if err := <-errc; err != nil {
			db.log.Error("Metrics collection failed", "err", err)
		}
	}
	var err error
	if db.db != nil {
		err = db.db.Close()
		if err == nil {
			db.log.Info("Database closed")
		} else {
			db.log.Error("Failed to close database", "err", err)
		}
	} else {
		err = db.db_p.Close()
	}
}

func (db *LDBDatabase) LDB() *leveldb.DB {
	return db.db
}

// Meter configures the database metrics collectors and

// meter periodically retrieves internal leveldb counters and reports them to
// the metrics subsystem.
//
// This is how a stats table look like (currently):
//
//	Compactions
//	 Level |   Tables   |    Size(MB)   |    Time(sec)  |    Read(MB)   |   Write(MB)
//	-------+------------+---------------+---------------+---------------+---------------
//	   0   |          0 |       0.00000 |       1.27969 |       0.00000 |      12.31098
//	   1   |         85 |     109.27913 |      28.09293 |     213.92493 |     214.26294
//	   2   |        523 |    1000.37159 |       7.26059 |      66.86342 |      66.77884
//	   3   |        570 |    1113.18458 |       0.00000 |       0.00000 |       0.00000
func (db *LDBDatabase) meter(refresh time.Duration) {
	// Create the counters to store current and previous values
	counters := make([][]float64, 2)
	for i := 0; i < 2; i++ {
		counters[i] = make([]float64, 3)
	}
	// Iterate ad infinitum and collect the stats
	for i := 1; ; i++ {
		// Retrieve the database stats
		stats, err := db.db.GetProperty("leveldb.stats")
		if err != nil {
			db.log.Error("Failed to read database stats", "err", err)
			return
		}
		// Find the compaction table, skip the header
		lines := strings.Split(stats, "\n")
		for len(lines) > 0 && strings.TrimSpace(lines[0]) != "Compactions" {
			lines = lines[1:]
		}
		if len(lines) <= 3 {
			db.log.Error("Compaction table not found")
			return
		}
		lines = lines[3:]

		// Iterate over all the table rows, and accumulate the entries
		for j := 0; j < len(counters[i%2]); j++ {
			counters[i%2][j] = 0
		}
		for _, line := range lines {
			parts := strings.Split(line, "|")
			if len(parts) != 6 {
				break
			}
			for idx, counter := range parts[3:] {
				value, err := strconv.ParseFloat(strings.TrimSpace(counter), 64)
				if err != nil {
					db.log.Error("Compaction entry parsing failed", "err", err)
					return
				}
				counters[i%2][idx] += value
			}
		}
		// Update all the requested meters
		if db.compTimeMeter != nil {
			db.compTimeMeter.Mark(int64((counters[i%2][0] - counters[(i-1)%2][0]) * 1000 * 1000 * 1000))
		}
		if db.compReadMeter != nil {
			db.compReadMeter.Mark(int64((counters[i%2][1] - counters[(i-1)%2][1]) * 1024 * 1024))
		}
		if db.compWriteMeter != nil {
			db.compWriteMeter.Mark(int64((counters[i%2][2] - counters[(i-1)%2][2]) * 1024 * 1024))
		}
		// Sleep a bit, then repeat the stats collection
		select {
		case errc := <-db.quitChan:
			// Quit requesting, stop hammering the database
			errc <- nil
			return

		case <-time.After(refresh):
			// Timeout, gather a new set of stats
		}
	}
}

//func (db *LDBDatabase) NewBatch() Batch {
//	return &ldbBatch{db: db.db, b: new(leveldb.Batch)}
//}

type ldbBatch struct {
	db   *leveldb.DB
	b    *leveldb.Batch
	size int
}

func (b *ldbBatch) Put(key, value []byte) error {
	b.b.Put(key, value)
	b.size += len(value)
	return nil
}

func (b *ldbBatch) Put_s(key, value []byte) error {
	b.b.Put(key, value)
	b.size += len(value)
	return nil
}

func (b *ldbBatch) Write() error {
	return b.db.Write(b.b, nil)
}
func (b *ldbBatch) Write_s() error {
	return b.db.Write(b.b, nil)
}
func (b *ldbBatch) ValueSize() int {
	return b.size
}

type table struct {
	db     Database
	prefix string
}

// NewTable returns a Database object that prefixes all keys with a given
// string.
//func NewTable(db Database, prefix string) Database {
//	return &table{
//		db:     db,
//		prefix: prefix,
//	}
//}

func (dt *table) Put(key []byte, value []byte) error {
	return dt.db.Put(append([]byte(dt.prefix), key...), value)
}
func (dt *table) Put_s(key []byte, value []byte) error {
	return dt.db.Put(append([]byte(dt.prefix), key...), value)
}

func (dt *table) Has(key []byte) (bool, error) {
	return dt.db.Has(append([]byte(dt.prefix), key...))
}

func (dt *table) Get(key []byte) ([]byte, error) {
	return dt.db.Get(append([]byte(dt.prefix), key...))
}
func (dt *table) Get_s(key []byte) ([]byte, error) {
	return dt.db.Get_s(append([]byte(dt.prefix), key...))
}
func (dt *table) Delete(key []byte) error {
	return dt.db.Delete(append([]byte(dt.prefix), key...))
}

func (dt *table) Close() {
	// Do nothing; don't close the underlying DB.
}

type tableBatch struct {
	batch  Batch
	prefix string
}

func (tb *tableBatch) Put_s(key []byte, value []byte) error {
	return tb.batch.Put(append([]byte(tb.prefix), key...), value)
}

func (tb *tableBatch) Write_s() error {
	return tb.batch.Write_s()
}

// NewTableBatch returns a Batch object which prefixes all keys with a given string.
//func NewTableBatch(db Database, prefix string) Batch {
//	return &tableBatch{db.NewBatch(), prefix}
//}

//func (dt *table) NewBatch() Batch {
//	return &tableBatch{dt.db.NewBatch(), dt.prefix}
//}

func (tb *tableBatch) Put(key, value []byte) error {
	return tb.batch.Put(append([]byte(tb.prefix), key...), value)
}

func (tb *tableBatch) Write() error {
	return tb.batch.Write()
}

func (tb *tableBatch) ValueSize() int {
	return tb.batch.ValueSize()
}

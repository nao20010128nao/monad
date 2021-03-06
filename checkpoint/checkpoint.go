package checkpoint

import (
	"fmt"
	flags "github.com/jessevdk/go-flags"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/btcsuite/goleveldb/leveldb"
	"github.com/wakiyamap/monad/chaincfg"
	"github.com/wakiyamap/monad/wire"
	"github.com/wakiyamap/monautil"
)

const (
	// userCheckpointDbNamePrefix is the prefix for the monad usercheckpoint database.
	userCheckpointDbNamePrefix = "usercheckpoints"

	// volatileCheckpointDbNamePrefix is the prefix for the monad volatilecheckpoint database.
	volatileCheckpointDbNamePrefix = "volatilecheckpoints"

	// AlertKeyDbNamePrefix is the prefix for the monad volatilecheckpoint database.
	alertKeyDbNamePrefix = "alertkey"

	defaultDbType = "leveldb"
)

var (
	monadHomeDir      = monautil.AppDataDir("monad", false)
	defaultDataDir    = filepath.Join(monadHomeDir, "data")
	defaultConfigFile = filepath.Join(monadHomeDir, "monad.conf")
	activeNetParams   = &chaincfg.MainNetParams

	// Default global config.
	cfg = &config{
		DataDir: filepath.Join(monadHomeDir, "data"),
	}
)

type UserCheckpoint struct {
	Ucdb *leveldb.DB
}

// config defines the global configuration options.
type config struct {
	DataDir        string `short:"b" long:"datadir" description:"Location of the monad data directory"`
	ConfigFile     string `short:"C" long:"configfile" description:"Path to configuration file"`
	TestNet4       bool   `long:"testnet" description:"Use the test network"`
	RegressionTest bool   `long:"regtest" description:"Use the regression test network"`
	SimNet         bool   `long:"simnet" description:"Use the simulation test network"`
}

var instance *UserCheckpoint
var once sync.Once

// netName returns the name used when referring to a bitcoin network.  At the
// time of writing, monad currently places blocks for testnet version 3 in the
// data and log directory "testnet", which does not match the Name field of the
// chaincfg parameters.  This function can be used to override this directory name
// as "testnet" when the passed active network matches wire.TestNet4.
//
// A proper upgrade to move the data and log directories for this network to
// "testnet4" is planned for the future, at which point this function can be
// removed and the network parameter's name used instead.
func netName(chainParams *chaincfg.Params) string {
	switch chainParams.Net {
	case wire.TestNet4:
		return "testnet"
	default:
		return chainParams.Name
	}
}

// loadConfig initializes and parses the config using a config file and command
// line options.
func loadConfig() (*config, []string, error) {
	// Default config.
	cfg := config{
		ConfigFile: defaultConfigFile,
	}

	// Load additional config from file.
	parser := flags.NewParser(&cfg, flags.Default)
	preCfg := cfg
	err := flags.NewIniParser(parser).ParseFile(preCfg.ConfigFile)
	remainingArgs, err := parser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); !ok || e.Type != flags.ErrHelp {
		}
		return nil, nil, err
	}

	// Multiple networks can't be selected simultaneously.
	numNets := 0
	if cfg.TestNet4 {
		numNets++
	}
	if cfg.SimNet {
		numNets++
	}
	if numNets > 1 {
		str := "%s: The testnet and simnet params can't be used " +
			"together -- choose one of the two"
		err := fmt.Errorf(str, "loadConfig")
		fmt.Fprintln(os.Stderr, err)
		return nil, nil, err
	}

	return &cfg, remainingArgs, nil
}

// open usercheckpointDB. Basically it is called only at startup.
func (uc *UserCheckpoint) OpenDB() error {
	if uc.Ucdb != nil {
		return nil
	}

	var err error
	dbpath := GetUserCheckpointDbPath()
	uc.Ucdb, err = leveldb.OpenFile(dbpath, nil)
	return err
}

// Basically it is called only at the end.
func (uc *UserCheckpoint) CloseDB() {
	if uc.Ucdb == nil {
		return
	}
	uc.Ucdb.Close()
	uc.Ucdb = nil
}

func (uc *UserCheckpoint) Add(height int64, hash string) {
	_ = uc.Ucdb.Put([]byte(fmt.Sprintf("%020d", height)), []byte(hash), nil)
}

func (uc *UserCheckpoint) Delete(height int64) {
	_ = uc.Ucdb.Delete([]byte(fmt.Sprintf("%020d", height)), nil)
}

func (uc *UserCheckpoint) GetMaxCheckpointHeight() (height int64) {
	height = 0
	iter := uc.Ucdb.NewIterator(nil, nil)
	iter.Last()

	if !iter.Valid() {
		return height
	}

	height, _ = strconv.ParseInt(string(iter.Key()), 10, 64)
	iter.Release()
	return height
}

func GetUserCheckpointDbInstance() *UserCheckpoint {
	once.Do(func() {
		time.Sleep(1 * time.Second)
		instance = &UserCheckpoint{nil}
	})
	return instance
}

func GetUserCheckpointDbPath() (dbPath string) {
	cfg, _, err := loadConfig()
	if err != nil {
		os.Exit(1)
	}
	if cfg.TestNet4 {
		activeNetParams = &chaincfg.TestNet4Params
	}
	if cfg.RegressionTest {
		activeNetParams = &chaincfg.RegressionNetParams
	}
	if cfg.SimNet {
		activeNetParams = &chaincfg.SimNetParams
	}
	dbName := userCheckpointDbNamePrefix + "_" + defaultDbType
	dbPath = filepath.Join(defaultDataDir, netName(activeNetParams), dbName)

	return dbPath
}

type VolatileCheckpoint struct {
	Vcdb *leveldb.DB
}

var vinstance *VolatileCheckpoint
var vonce sync.Once

// open volatilecheckpointDB. Basically it is called only at startup.
func (vc *VolatileCheckpoint) OpenDB() error {
	if vc.Vcdb != nil {
		return nil
	}

	var err error
	dbpath := GetVolatileCheckpointDbPath()
	vc.Vcdb, err = leveldb.OpenFile(dbpath, nil)
	return err
}

// close volatilecheckpointDB. Basically it is called only at the end.
func (vc *VolatileCheckpoint) CloseDB() {
	if vc.Vcdb == nil {
		return
	}
	vc.Vcdb.Close()
	vc.Vcdb = nil
}

func (vc *VolatileCheckpoint) Set(height int64, hash string) {
	_ = vc.Vcdb.Put([]byte(fmt.Sprintf("%020d", height)), []byte(hash), nil)
}

func (vc *VolatileCheckpoint) ClearDB() {
	iter := vc.Vcdb.NewIterator(nil, nil)
	for iter.Next() {
		err := vc.Vcdb.Delete([]byte(string(iter.Key())), nil)
		if err != nil {
			break
		}
	}
	iter.Release()
}

func GetVolatileCheckpointDbInstance() *VolatileCheckpoint {
	vonce.Do(func() {
		time.Sleep(1 * time.Second)
		vinstance = &VolatileCheckpoint{nil}
	})
	return vinstance
}

func GetVolatileCheckpointDbPath() (dbPath string) {
	cfg, _, err := loadConfig()
	if err != nil {
		os.Exit(1)
	}
	if cfg.TestNet4 {
		activeNetParams = &chaincfg.TestNet4Params
	}
	if cfg.RegressionTest {
		activeNetParams = &chaincfg.RegressionNetParams
	}
	if cfg.SimNet {
		activeNetParams = &chaincfg.SimNetParams
	}
	dbName := volatileCheckpointDbNamePrefix + "_" + defaultDbType
	dbPath = filepath.Join(defaultDataDir, netName(activeNetParams), dbName)

	return dbPath
}

type AlertKey struct {
	Akdb *leveldb.DB
}

var ainstance *AlertKey
var aonce sync.Once

func (ak *AlertKey) OpenDB() error {
	if ak.Akdb != nil {
		return nil
	}
	var err error
	dbpath := GetAlertKeyDbPath()
	ak.Akdb, err = leveldb.OpenFile(dbpath, nil)
	return err
}

func (ak *AlertKey) CloseDB() {
	if ak.Akdb == nil {
		return
	}
	ak.Akdb.Close()
	ak.Akdb = nil
}

// Alertkey is disabled when you came here.irreversible.
func (ak *AlertKey) Set(key string) {
	_ = ak.Akdb.Put([]byte(key), []byte("true"), nil)
}

// Add alertkey if it is not in database.
// Returns true if both of the public keys alertkey are OK.
func (ak *AlertKey) IsValid() bool {
	d1, err := ak.Akdb.Get(activeNetParams.AlertPubMainKey, nil)
	if err != nil {
		_ = ak.Akdb.Put(activeNetParams.AlertPubMainKey, []byte("false"), nil)
	}

	d2, err := ak.Akdb.Get(activeNetParams.AlertPubSubKey, nil)
	if err != nil {
		_ = ak.Akdb.Put(activeNetParams.AlertPubSubKey, []byte("false"), nil)
	}

	if string(d1) == "false" && string(d2) == "false" {
		return true
	}
	return false
}

func GetAlertKeyDbInstance() *AlertKey {
	aonce.Do(func() {
		time.Sleep(1 * time.Second)
		ainstance = &AlertKey{nil}
	})
	return ainstance
}

func GetAlertKeyDbPath() (dbPath string) {
	cfg, _, err := loadConfig()
	if err != nil {
		os.Exit(1)
	}
	if cfg.TestNet4 {
		activeNetParams = &chaincfg.TestNet4Params
	}
	if cfg.RegressionTest {
		activeNetParams = &chaincfg.RegressionNetParams
	}
	if cfg.SimNet {
		activeNetParams = &chaincfg.SimNetParams
	}
	dbName := alertKeyDbNamePrefix + "_" + defaultDbType
	dbPath = filepath.Join(defaultDataDir, netName(activeNetParams), dbName)
	return dbPath
}

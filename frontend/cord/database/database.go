package database

import (
	"github.com/ProtocolONE/chihaya/frontend/cord/config"
	"github.com/ProtocolONE/chihaya/frontend/cord/models"

	"go.uber.org/zap"
	mgo "gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"sync"
	"time"
)

type DbConf struct {
	Dbs      *mgo.Session
	Database string
}

var dbConf *DbConf

func Init() error {

	cfg := config.Get().Database

	dbConf = &DbConf{
		Database: cfg.Database,
	}

	timeout, _ := time.ParseDuration("30s")
	session, err := mgo.DialWithInfo(&mgo.DialInfo{
		Addrs:    []string{cfg.Host},
		Database: cfg.Database,
		Username: cfg.User,
		Password: cfg.Password,
		Timeout: timeout,
	})

	if err != nil {
		session, err := mgo.DialWithTimeout(cfg.Host, timeout)
		if err != nil {
			zap.S().Fatal(err)
			return err
		}

		db := session.DB(cfg.Database)
		err = db.Login(cfg.User, cfg.Password)
		if err != nil {
			zap.S().Fatal(err)
			return err
		}
	}

	dbConf.Dbs = session
	zap.S().Infof("Connected to DB: \"%s\" [u:\"%s\":p\"%s\"]", dbConf.Database, cfg.User, cfg.Password)

	return nil
}

type UserManager struct {
	collection *mgo.Collection
}

func NewUserManager() *UserManager {
	session := dbConf.Dbs.Copy()
	return &UserManager{collection: session.DB(dbConf.Database).C("users")}
}

func (manager *UserManager) FindByName(name string) ([]*models.User, error) {

	var dbUsers []*models.User
	err := manager.collection.Find(bson.M{"username": name}).All(&dbUsers)
	if err != nil {
		return nil, err
	}

	return dbUsers, nil
}

func (manager *UserManager) RemoveByName(name string) error {

	err := manager.collection.Remove(bson.M{"username": name})
	if err != nil {
		return err
	}

	return nil
}

func (manager *UserManager) Insert(user *models.User) error {

	err := manager.collection.Insert(user)
	if err != nil {
		return err
	}

	return nil
}

type TorrentManager struct {
	collection *mgo.Collection
}

func NewTorrentManager() *TorrentManager {
	session := dbConf.Dbs.Copy()
	return &TorrentManager{collection: session.DB(dbConf.Database).C("torrents")}
}

func (manager *TorrentManager) Insert(torrent *models.Torrent) error {

	err := manager.collection.Insert(torrent)
	if err != nil {
		return err
	}

	return nil
}

func (manager *TorrentManager) RemoveByInfoHash(infoHash string) error {

	err := manager.collection.Remove(bson.M{"info_hash": infoHash})
	if err != nil {
		return err
	}

	return nil
}

func (manager *TorrentManager) FindByInfoHash(infoHash string) ([]*models.Torrent, error) {

	var dbTorrent []*models.Torrent
	err := manager.collection.Find(bson.M{"info_hash": infoHash}).All(&dbTorrent)
	if err != nil {
		return nil, err
	}

	return dbTorrent, nil
}

func (manager *TorrentManager) FindAll() ([]*models.Torrent, error) {

	var dbTorrent []*models.Torrent
	err := manager.collection.Find(nil).All(&dbTorrent)
	if err != nil {
		return nil, err
	}

	return dbTorrent, nil
}

type MemTorrentManager struct {
	collection sync.Map
}

func NewMemTorrentManager() *MemTorrentManager {
	return &MemTorrentManager{}
}

var _memTorrentManager = NewMemTorrentManager()

func GetMemTorrentManager() *MemTorrentManager {
	return _memTorrentManager
}

func (manager *MemTorrentManager) Insert(torrent *models.Torrent) {

	manager.collection.Store(torrent.InfoHash, torrent)
}

func (manager *MemTorrentManager) RemoveByInfoHash(infoHash string) {

	manager.collection.Delete(infoHash)
}

func (manager *MemTorrentManager) FindByInfoHash(infoHash string) *models.Torrent {

	v, ok := manager.collection.Load(infoHash)
	if !ok {
		return nil
	}

	return v.(*models.Torrent)
}

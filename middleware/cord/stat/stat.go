package stat

import (
	"context"
	"fmt"
	"time"
	"net/url"
	"errors"
	"strings"
	"strconv"
	"encoding/binary"

	yaml "gopkg.in/yaml.v2"

	"github.com/ProtocolONE/chihaya/bittorrent"
	"github.com/ProtocolONE/chihaya/middleware"
	"github.com/ProtocolONE/chihaya/pkg/log"
	"github.com/gomodule/redigo/redis"
	"github.com/ProtocolONE/chihaya/frontend/udp"
)

// Name is the name by which this middleware is registered with Chihaya.
const Name = "stat"

func Init() {
	middleware.RegisterDriver(Name, driver{})
}

var _ middleware.Driver = driver{}

type driver struct{}

func (d driver) NewHook(optionBytes []byte) (middleware.Hook, error) {
	var cfg Config
	err := yaml.Unmarshal(optionBytes, &cfg)
	if err != nil {
		return nil, fmt.Errorf("invalid options for middleware %s: %s", Name, err)
	}

	return NewHook(cfg)
}

var ErrTorrentUnapproved = bittorrent.ClientError("unapproved torrent")

type Config struct {
	RedisUrl string `yaml:"redis_broker"`
}

func (cfg Config) LogFields() log.Fields {

	return log.Fields{
		"redis_broker": cfg.RedisUrl,
	}
}

type hook struct {
	cfg     Config
	redisPool *redis.Pool
	gen			*udp.ConnectionIDGenerator
}

func NewHook(cfg Config) (middleware.Hook, error) {

	var u *url.URL
	u, err := url.Parse(cfg.RedisUrl)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "redis" {
		return nil, errors.New("no redis scheme found")
	}

	db := 0 //default redis db
	parts := strings.Split(u.Path, "/")
	if len(parts) != 1 {
		db, err = strconv.Atoi(parts[1])
		if err != nil {
			return nil, err
		}
	}

	redisPool := &redis.Pool{
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
		Dial: func() (redis.Conn, error) {

			var opts = []redis.DialOption{
				redis.DialDatabase(db),
			}

			if u.User.String() != "" {
				opts = append(opts, redis.DialPassword(u.User.String()))
			}

			c, err := redis.Dial("tcp", u.Host, opts...)
			if err != nil {
				return nil, err
			}
			return c, err
		},
	}

	h := &hook{
		cfg:     cfg,
		redisPool: redisPool,
		gen: udp.NewConnectionIDGenerator(""),
	}

	return h, nil
}

type statInfo struct {
	downloaded     uint64
	uploaded       uint64
	left           uint64
	connId		   uint64
	userId   	   string
	infoHash       string
	status         uint8
	timestamp      time.Time
}

func (h *hook) saveStat(si statInfo) error {

	redisConn := h.redisPool.Get()
	defer redisConn.Close()

	stats := []string{"none", "completed", "started", "stopped"}

	field := fmt.Sprintf("%s.%d", si.infoHash, si.connId)
	_, err := redisConn.Do("HMSET", si.userId, 
		field + ".downloaded", si.downloaded, 
		field + ".left", si.left, 
		field + ".uploaded", si.uploaded, 
		field + ".status", stats[si.status], 
		field + ".time", si.timestamp)

	if err != nil {
		fmt.Println(err.Error())
		return err
	}

	_, err = redisConn.Do("SADD", "lastUpdated", si.userId)
	return err
}

func (h *hook) HandleAnnounce(ctx context.Context, req *bittorrent.AnnounceRequest, resp *bittorrent.AnnounceResponse) (context.Context, error) {

	var si statInfo

	si.downloaded = req.Downloaded
	si.uploaded = req.Uploaded
	si.left = req.Left
	si.status = uint8(req.Event)
	si.infoHash = req.InfoHash.String()
	si.userId = req.Peer.ID.String()
	si.connId = binary.BigEndian.Uint64(h.gen.Generate(req.Peer.IP.IP, time.Now()))
	si.timestamp = time.Now()

	h.saveStat(si)

	return ctx, nil
}

func (h *hook) HandleScrape(ctx context.Context, req *bittorrent.ScrapeRequest, resp *bittorrent.ScrapeResponse) (context.Context, error) {
	return ctx, nil
}

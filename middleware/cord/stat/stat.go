package stat

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	yaml "gopkg.in/yaml.v2"

	"github.com/ProtocolONE/chihaya/bittorrent"
	"github.com/ProtocolONE/chihaya/frontend/udp"
	"github.com/ProtocolONE/chihaya/middleware"
	"github.com/ProtocolONE/chihaya/pkg/log"
	"github.com/gomodule/redigo/redis"
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
	cfg       Config
	redisPool *redis.Pool
	gen       *udp.ConnectionIDGenerator
	ticker    *time.Ticker
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
		cfg:       cfg,
		redisPool: redisPool,
		gen:       udp.NewConnectionIDGenerator(""),
		ticker:    time.NewTicker(time.Hour),
	}

	go func() {
		for _ = range h.ticker.C {
			h.collectStat()
		}
	}()

	h.collectStat()
	return h, nil
}

type statInfo struct {
	downloaded uint64
	uploaded   uint64
	left       uint64
	connId     uint32
	userId     string
	infoHash   string
	status     uint8
	timestamp  time.Time
}

func hGetAll(redisConn redis.Conn, key string) (interface{}, error) {

	data, err := redisConn.Do("HGETALL", key)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func loadUserData(redisConn redis.Conn, userId string) (map[string]map[string]map[string]string, error) {

	reply, err := hGetAll(redisConn, userId)
	userData, err := redis.ByteSlices(reply, err)
	if err != nil {
		return nil, err
	}

	if len(userData)%2 != 0 {
		return nil, nil
	}

	events := make(map[string]map[string]map[string]string)
	for i := 0; i < len(userData); i += 2 {

		parts := strings.Split(string(userData[i]), ".")
		if len(parts) != 3 {
			continue
		}

		infoHash := parts[0]
		sessionId := parts[1]
		name := parts[2]
		if events[infoHash] == nil {
			events[infoHash] = make(map[string]map[string]string)
		}
		if events[infoHash][sessionId] == nil {
			events[infoHash][sessionId] = make(map[string]string)
		}
		events[infoHash][sessionId][name] = string(userData[i+1])
	}

	return events, nil
}

func (h *hook) collectStat() error {

	redisConn := h.redisPool.Get()
	defer redisConn.Close()

	reply, err := redisConn.Do("SMEMBERS", "lastUpdated")
	lastUpdated, err := redis.Strings(reply, err)
	if err != nil {
		return err
	}

	for _, userId := range lastUpdated {

		events, err := loadUserData(redisConn, userId)
		if err != nil {
			return err
		}

		for hashinfo, sessions := range events {

			reply, err = redisConn.Do("HGET", "hashinfo", hashinfo)
			gameId, err := redis.String(reply, err)
			if err != nil {
				return err
			}

			for _, names := range sessions {

				status := names["status"]
				if status == "stopped" {
					uploaded := names["uploaded"]
					downloaded := names["downloaded"]
					totalKey := "total:" + userId

					redisConn.Send("MULTI")
					redisConn.Send("ZINCRBY", "rating:downloaded", downloaded, userId)
					redisConn.Send("ZINCRBY", "rating:uploaded", uploaded, userId)
					redisConn.Send("ZINCRBY", "rating:downloaded:"+gameId, downloaded, userId)
					redisConn.Send("ZINCRBY", "rating:uploaded:"+gameId, uploaded, userId)
					redisConn.Send("HINCRBY", totalKey, "uploaded", uploaded)
					redisConn.Send("HINCRBY", totalKey, "downloaded", downloaded)
					redisConn.Send("HINCRBY", totalKey, "uploaded:"+gameId, uploaded)
					redisConn.Send("HINCRBY", totalKey, "downloaded:"+gameId, downloaded)

					_, err = redisConn.Do("EXEC")
					if err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

func (h *hook) saveStat(si statInfo) error {

	redisConn := h.redisPool.Get()
	defer redisConn.Close()

	stats := []string{"none", "completed", "started", "stopped"}
	field := fmt.Sprintf("%s.%d", si.infoHash, si.connId)

	err := redisConn.Send("MULTI")
	err = redisConn.Send("HMSET", si.userId,
		field+".downloaded", si.downloaded,
		field+".left", si.left,
		field+".uploaded", si.uploaded,
		field+".status", stats[si.status],
		field+".time", si.timestamp)

	err = redisConn.Send("SADD", "lastUpdated", si.userId)

	_, err = redisConn.Do("EXEC")
	return err
}

func (h *hook) HandleAnnounce(ctx context.Context, req *bittorrent.AnnounceRequest, resp *bittorrent.AnnounceResponse) (context.Context, error) {

	var si statInfo

	if len(req.UserID) > 0 && req.TransactionID > 0 {

		si.downloaded = req.Downloaded
		si.uploaded = req.Uploaded
		si.left = req.Left
		si.status = uint8(req.Event)
		si.infoHash = req.InfoHash.String()
		si.userId = req.UserID
		si.connId = req.TransactionID
		si.timestamp = time.Now()

		h.saveStat(si)
	}

	return ctx, nil
}

func (h *hook) HandleScrape(ctx context.Context, req *bittorrent.ScrapeRequest, resp *bittorrent.ScrapeResponse) (context.Context, error) {
	return ctx, nil
}

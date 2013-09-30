package main

import (
	metrictools "../"
	"flag"
	nsq "github.com/bitly/go-nsq"
	"github.com/garyburd/redigo/redis"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
)

var (
	conf_file = flag.String("conf", "metrictools.json", "metrictools config file")
)

func main() {
	flag.Parse()
	c, err := metrictools.ReadConfig(*conf_file)
	if err != nil {
		log.Fatal("config parse error", err)
	}
	lookupd_addresses, _ := c["lookupd_addresses"]
	nsqd_addr, _ := c["nsqd_addr"]
	maxinflight, _ := c["maxinflight"]
	metric_channel, _ := c["metric_channel"]
	metric_topic, _ := c["metric_topic"]
	trigger_topic, _ := c["trigger_topic"]
	archive_topic, _ := c["archive_topic"]
	redis_server, _ := c["data_redis_server"]
	redis_auth, _ := c["data_redis_auth"]
	config_redis_server, _ := c["config_redis_server"]
	config_redis_auth, _ := c["config_redis_auth"]

	redis_con := func() (redis.Conn, error) {
		c, err := redis.Dial("tcp", redis_server)
		if err != nil {
			return nil, err
		}
		if _, err := c.Do("AUTH", redis_auth); err != nil {
			c.Close()
			return nil, err
		}
		return c, err
	}
	redis_pool := redis.NewPool(redis_con, 3)
	defer redis_pool.Close()

	config_redis_con := func() (redis.Conn, error) {
		c, err := redis.Dial("tcp", config_redis_server)
		if err != nil {
			return nil, err
		}
		if _, err := c.Do("AUTH", config_redis_auth); err != nil {
			c.Close()
			return nil, err
		}
		return c, err
	}
	config_redis_pool := redis.NewPool(config_redis_con, 3)
	defer config_redis_pool.Close()

	w := nsq.NewWriter(nsqd_addr)
	metric_deliver := &MetricDeliver{
		dataservice:   redis_pool,
		configservice: config_redis_pool,
		writer:        w,
		trigger_topic: trigger_topic,
		archive_topic: archive_topic,
		nsqd_addr:     nsqd_addr,
	}
	max, _ := strconv.ParseInt(maxinflight, 10, 32)
	r, err := nsq.NewReader(metric_topic, metric_channel)
	if err != nil {
		log.Fatal(err)
	}
	r.SetMaxInFlight(int(max))
	for i := 0; i < int(max); i++ {
		r.AddHandler(metric_deliver)
	}
	lookupdlist := strings.Split(lookupd_addresses, ",")
	for _, addr := range lookupdlist {
		log.Printf("lookupd addr %s", addr)
		err := r.ConnectToLookupd(addr)
		if err != nil {
			log.Fatal(err)
		}
	}
	go metric_deliver.ScanTrigger()
	termchan := make(chan os.Signal, 1)
	signal.Notify(termchan, syscall.SIGINT, syscall.SIGTERM)
	<-termchan
	r.Stop()
	w.Stop()
}
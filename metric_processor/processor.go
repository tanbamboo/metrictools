package main

import (
	metrictools "../"
	"encoding/json"
	"fmt"
	"github.com/bitly/nsq/nsq"
	"github.com/garyburd/redigo/redis"
	"log"
	"time"
)

type MsgDeliver struct {
	dataservice   *redis.Pool
	configservice *redis.Pool
	writer        *nsq.Writer
	trigger_topic string
	archive_topic string
	nsqd_addr     string
}

func (this *MsgDeliver) HandleMessage(m *nsq.Message) error {
	var err error
	var c []metrictools.CollectdJSON
	if err = json.Unmarshal(m.Body, &c); err != nil {
		log.Println(err)
		return nil
	}
	for _, v := range c {
		if len(v.Values) != len(v.DSNames) {
			continue
		}
		if len(v.Values) != len(v.DSTypes) {
			continue
		}
		msgs := v.ToRecord()
		if err := this.PersistData(msgs); err != nil {
			return err
		}
	}
	return nil
}

func (this *MsgDeliver) PersistData(msgs []*metrictools.Record) error {
	var err error
	data_con := this.dataservice.Get()
	defer data_con.Close()
	config_con := this.configservice.Get()
	defer config_con.Close()
	for _, msg := range msgs {
		var new_value float64
		if msg.DSType == "counter" || msg.DSType == "derive" {
			new_value, err = this.getRate(msg)
		} else {
			new_value = msg.Value
		}
		if err != nil && err.Error() == "ignore" {
			continue
		}
		if err != nil {
			log.Println("fail to get new value", err)
			return err
		}
		_, err := data_con.Do("ZADD", "archive:"+msg.Key,
			msg.Timestamp, new_value)
		if err != nil {
			log.Println(err)
			break
		}
		t, _ := redis.Float64(config_con.Do("GET", "archivetime:"+msg.Key))
		if time.Now().Unix()-int64(t) > 600 {
			this.writer.Publish(this.archive_topic, []byte(msg.Key))
		}
		body := fmt.Sprintf("%d:%.2f", msg.Timestamp, msg.Value)
		_, err = data_con.Do("SET", "raw:"+msg.Key, body)
		if err != nil {
			log.Println("set raw", err)
			break
		}
		_, err = data_con.Do("SET", msg.Key, new_value)
		if err != nil {
			log.Println("last data", err)
			break
		}
		_, err = config_con.Do("SADD", msg.Host, msg.Key)
	}
	return err
}

func (this *MsgDeliver) getRate(msg *metrictools.Record) (float64, error) {
	data_con := this.dataservice.Get()
	defer data_con.Close()
	rst, err := redis.String(data_con.Do("GET", "raw:"+msg.Key))
	if err != nil {
		return 0, err
	}
	var value float64
	t, v, err := metrictools.GetTimestampAndValue(rst)
	if err == nil {
		value = (msg.Value - v) / float64(msg.Timestamp-t)
	} else {
		value = msg.Value
	}
	if value < 0 {
		value = 0
	}
	return value, nil
}

func (this *MsgDeliver) ScanTrigger() {
	ticker := time.Tick(time.Minute)
	config_con := this.configservice.Get()
	defer config_con.Close()
	for {
		keys, err := redis.Strings(config_con.Do("KEYS", "trigger:*"))
		if err != nil {
			continue
		}
		for _, v := range keys {
			last, err := redis.Int64(config_con.Do("HGET", v, "last"))
			if err != nil {
				continue
			}
			now := time.Now().Unix()
			if now-last < 61 {
				continue
			}
			_, _, err = this.writer.Publish(this.trigger_topic, []byte(v))
		}
		<-ticker
	}
}

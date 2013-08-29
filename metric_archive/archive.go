package main

import (
	metrictools "../"
	"github.com/bitly/nsq/nsq"
	"github.com/garyburd/redigo/redis"
	"log"
	"time"
)

type DataArchive struct {
	dataservice   *redis.Pool
	configservice *redis.Pool
}

func (this *DataArchive) HandleMessage(m *nsq.Message) error {
	config_con := this.configservice.Get()
	defer config_con.Close()
	stat, _ := redis.Int(config_con.Do("GET", "setting:"+string(m.Body)))
	var last int64
	current := time.Now().Unix()
	if stat > 0 {
		last = current - int64(stat)*24*3600
	} else {
		last = current - 300
	}
	if stat == -1 {
		last = current - 60*24*3600
	}
	metric := "archive:" + string(m.Body)
	data_con := this.dataservice.Get()
	defer data_con.Close()
	_, err := data_con.Do("ZREMRANGEBYSCORE", metric, 0, last)
	if err != nil {
		log.Println("last data", err)
		return err
	}
	config_con.Do("SET", "archivetime:"+string(m.Body), time.Now().Unix())
	if stat != 0 {
		go this.do_compress(metric)
	}
	return nil
}

func (this *DataArchive) do_compress(key string) {
	config_con := this.configservice.Get()
	defer config_con.Close()
	t, err := redis.Float64(config_con.Do("GET", "compresstime:"+key))
	if err != nil {
		return
	}
	current := int64(t)
	last_d := time.Now().Unix() - 24*3600
	last_2d := time.Now().Unix() - 2*24*3600
	var interval int64
	data_con := this.dataservice.Get()
	defer data_con.Close()
	for {
		if current > last_d {
			break
		}
		if current < last_2d {
			interval = 600
		} else {
			interval = 300
		}
		rst, err := data_con.Do("ZRANGEBYSCORE", key, current, current+interval)
		if err == nil {
			value_list := rst.([]interface{})
			sumvalue := float64(0)
			sumtime := int64(0)
			for _, value := range value_list {
				t, v, _ := metrictools.GetTimestampAndValue(string(value.([]byte)))
				sumvalue += v
				sumtime += t
			}
			size := len(value_list)
			if size > 0 {
				_, err = data_con.Do("ZADD", key, sumtime/int64(size), sumvalue/float64(size))
				if err != nil {
					break
				}
			}
			for _, value := range value_list {
				_, err = data_con.Do("ZREM", key, value)
				if err != nil {
					break
				}
			}
			if err != nil {
				break
			}
			current += interval
		} else {
			log.Println("fail to get range", err)
			time.Sleep(time.Second)
		}
	}
	config_con.Do("SET", "compresstime:"+key, current)
}

package metrictools

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/datastream/nsq/nsq"
	"github.com/garyburd/redigo/redis"
	"labix.org/v2/mgo"
	"log"
	"strconv"
	"strings"
	"time"
)

type Message struct {
	*nsq.Message
	ResponseChannel chan *nsq.FinishedMessage
}

type MsgDeliver struct {
	MessageChan    chan *Message
	MSession       *mgo.Session
	DBName         string
	RedisChan      chan *RedisOP
	RedisPool      *redis.Pool
	VerboseLogging bool
}

type RedisOP struct {
	Action string
	Key    string
	Value  interface{}
	Result interface{}
	Err    error
	Done   chan int
}

func (this *MsgDeliver) ParseJSON(c CollectdJSON) []*Record {
	keys := c.GenNames()
	var msgs []*Record
	for i := range c.Values {
		msg := &Record{
			Host:      c.Host,
			Key:       c.Host + "_" + keys[i],
			Value:     c.Values[i],
			Timestamp: int64(c.TimeStamp),
			TTL:       int(c.Interval) * 3 / 2,
			DSType:    c.DSTypes[i],
			Interval:  c.Interval,
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

func (this *MsgDeliver) HandleMessage(m *nsq.Message, r chan *nsq.FinishedMessage) {
	this.MessageChan <- &Message{m, r}
}

func (this *MsgDeliver) ProcessData() {
	for {
		m := <-this.MessageChan
		go this.insert_data(m)
	}
}

func (this *MsgDeliver) insert_data(m *Message) {
	var err error
	var c []CollectdJSON
	if err = json.Unmarshal(m.Body, &c); err != nil {
		m.ResponseChannel <- &nsq.FinishedMessage{
			m.Id, 0, true}
		log.Println(err)
		return
	}
	if this.VerboseLogging {
		log.Println("RAW JSON String: ", string(m.Body))
		log.Println("JSON SIZE: ", len(c))
	}
	stat := true
	for _, v := range c {
		if len(v.Values) != len(v.DSNames) {
			continue
		}
		if len(v.Values) != len(v.DSTypes) {
			continue
		}
		msgs := this.ParseJSON(v)
		if err := this.PersistData(msgs); err != nil {
			stat = false
			break
		}
	}
	m.ResponseChannel <- &nsq.FinishedMessage{m.Id, 0, stat}
}

func (this *MsgDeliver) PersistData(msgs []*Record) error {
	session := this.MSession.Copy()
	defer session.Close()
	var err error
	for _, msg := range msgs {
		var new_value float64
		if msg.DSType == "counter" || msg.DSType == "derive" {
			new_value, err = this.gen_new_value(msg)
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
		n_v := &KeyValue{
			Timestamp: msg.Timestamp,
			Value:     new_value,
		}
		err = this.UpdateValue("ZADD", "archive:"+msg.Key, n_v)
		if err != nil {
			log.Println(err)
			break
		}
		body := fmt.Sprintf("%d:%.2f", msg.Timestamp, msg.Value)
		err = this.UpdateValue("SET", "raw:"+msg.Key, body)
		if err != nil {
			log.Println("set raw", err)
			break
		}
		err = this.UpdateValue("SET", msg.Key, new_value)
		if err != nil {
			log.Println("last data", err)
			break
		}
	}
	return err
}

func (this *MsgDeliver) ExpireData() {
	for {
		op := &RedisOP{
			Action: "KEYS",
			Key:    "archiv:*",
			Done:   make(chan int),
		}
		this.RedisChan <- op
		<-op.Done
		if op.Err == nil {
			value_list := op.Result.([]interface{})
			for _, value := range value_list {
				this.remove_dup(value.([]byte))
			}
		} else {
			time.Sleep(time.Minute)
			continue
		}
	}
}

func (this *MsgDeliver) remove_dup(key []byte) {
	index := 0
	count := this.GetSetSize(string(key))
	for {
		if index > count {
			break
		}
		op := &RedisOP{
			Action: "ZRANG",
			Key:    string(key),
			Value:  []interface{}{index, index + 5},
			Done:   make(chan int),
		}
		this.RedisChan <- op
		<-op.Done
		if op.Err == nil {
			value_list := op.Result.([]interface{})
			var last float64
			last = 0
			for _, value := range value_list {
				v := string(value.([]byte))
				d, _ := GetValue(v)
				if d != last {
					last = d
					continue
				}
				this.UpdateValue("ZREM", string(key), v)
				count--
				index--
			}
		}
		index = index + 5
	}
}

func (this *MsgDeliver) GetSetSize(key string) int {
	op := &RedisOP{
		Action: "ZCARD",
		Key:    key,
		Done:   make(chan int),
	}
	this.RedisChan <- op
	<-op.Done
	count := 0
	if op.Err == nil {
		v := op.Result.([]byte)
		t, err := strconv.ParseInt(string(v), 10, 32)
		if err == nil {
			count = int(t)
		}
	}
	return count
}

func (this *MsgDeliver) UpdateValue(action string, key string, value interface{}) error {
	op := &RedisOP{
		Action: action,
		Key:    key,
		Value:  value,
		Done:   make(chan int),
	}
	this.RedisChan <- op
	<-op.Done
	return op.Err
}

func GetTimestamp(key string) (int64, error) {
	t, _, err := GetTimestampValue(key)
	return t, err
}

func GetValue(key string) (float64, error) {
	_, v, err := GetTimestampValue(key)
	return v, err
}

func GetTimestampValue(key string) (int64, float64, error) {
	body := string(key)
	kv := strings.Split(body, ":")
	var t int64
	var v float64
	var err error
	if len(kv) == 2 {
		t, err = strconv.ParseInt(kv[0], 10, 64)
		v, err = strconv.ParseFloat(kv[1], 64)
		if err != nil {
			log.Println(kv, err)
		}
	} else {
		err = errors.New("wrong data")
	}
	return t, v, err
}

func (this *MsgDeliver) Redis() {
	redis_con := this.RedisPool.Get()
	for {
		op := <-this.RedisChan
		switch op.Action {
		case "ZCARD":
			fallthrough
		case "KEYS":
			fallthrough
		case "GET":
			op.Result, op.Err = redis_con.Do(op.Action,
				op.Key)
		case "ZREM":
			op.Result, op.Err = redis_con.Do(op.Action,
				op.Key, op.Value)
		case "ZADD":
			v := op.Value.(*KeyValue)
			body := fmt.Sprintf("%d:%.2f", v.Timestamp, v.Value)
			op.Result, op.Err = redis_con.Do(op.Action,
				op.Key, v.Timestamp, body)
		case "ZRANGEBYSCORE":
			fallthrough
		case "ZREMRANGEBYSCORE":
			v := op.Value.([]interface{})
			if len(v) < 2 {
				op.Err = errors.New("wrong arg")
			} else {
				op.Result, op.Err = redis_con.Do(op.Action,
					op.Key, v[0], v[1])
			}
		default:
			op.Result, op.Err = redis_con.Do(op.Action,
				op.Key, op.Value)
		}
		if op.Err != nil {
			redis_con = this.RedisPool.Get()
		}
		op.Done <- 1
	}
}

func (this *MsgDeliver) gen_new_value(msg *Record) (float64, error) {
	op := &RedisOP{
		Action: "GET",
		Key:    "raw:" + msg.Key,
		Done:   make(chan int),
	}
	this.RedisChan <- op
	<-op.Done
	if op.Err != nil {
		return 0, op.Err
	}
	if op.Result == nil {
		return msg.Value, nil
	}
	var value float64
	t, v, err := GetTimestampValue(string(op.Result.([]byte)))
	if err == nil {
		value = (msg.Value - v) /
			float64(msg.Timestamp-t)
	} else {
		value = msg.Value
	}
	if value < 0 {
		value = 0
	}
	return value, nil
}

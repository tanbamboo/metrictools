package main

import (
	"encoding/json"
	"github.com/garyburd/redigo/redis"
	"github.com/gorilla/mux"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
)

func MetricHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=\"utf-8\"")
	metrics := r.FormValue("metrics")
	starttime := r.FormValue("starttime")
	endtime := r.FormValue("endtime")
	start := gettime(starttime)
	end := gettime(endtime)
	if !checktime(start, end) {
		start = end - 3600*3
	}

	metric_list := strings.Split(metrics, ",")
	record_list := make(map[string][]interface{})
	data_con := dataservice.Get()
	defer data_con.Close()
	for _, v := range metric_list {
		metric_data, err := redis.Strings(data_con.Do("ZRANGEBYSCORE", "archive:"+v, start, end))
		if err != nil {
			log.Println(err)
			continue
		}
		var kv []interface{}
		for _, item := range metric_data {
			t_v := strings.Split(item, ":")
			if len(t_v) != 2 {
				log.Println("error redis data")
				continue
			}
			t, _ := strconv.ParseInt(t_v[0], 10, 64)
			value, _ := strconv.ParseFloat(t_v[1], 64)
			kv = append(kv, []interface{}{t, value})
		}
		record_list[v] = kv
	}
	w.Write(gen_json(record_list))
}

func MetricCreateHandler(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to read request"))
		log.Println(err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=\"utf-8\"")
	var metrics map[string]int
	if err = json.Unmarshal(body, &metrics); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("failed to parse json"))
		return
	} else {
		w.WriteHeader(http.StatusOK)
	}
	config_con := configservice.Get()
	defer config_con.Close()
	data_con := dataservice.Get()
	defer data_con.Close()
	for metric, value := range metrics {
		v, _ := data_con.Do("GET", "archive:"+metric)
		if v != nil {
			config_con.Do("SET", "setting:"+metric, value)
		}
	}
}

func MetricDeleteHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=\"utf-8\"")
	w.WriteHeader(http.StatusOK)
	metric := mux.Vars(r)["name"]
	config_con := configservice.Get()
	defer config_con.Close()
	data_con := dataservice.Get()
	defer data_con.Close()
	data_con.Do("DEL", "archive:"+metric)
	data_con.Do("DEL", metric)
	config_con.Do("DEL", "setting:"+metric)
}
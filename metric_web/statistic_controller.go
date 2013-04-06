package main

import (
	"github.com/gorilla/mux"
	"log"
	"net/http"
	"strconv"
	"strings"
)

func StatisticHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=\"utf-8\"")
	name := mux.Vars(r)["name"]
	starttime := r.FormValue("start")
	endtime := r.FormValue("end")
	start := gettime(starttime)
	end := gettime(endtime)
	if !checktime(start, end) {
		start = end - 3600*3
	}
	record_list := make(map[string][]interface{})
	redis_con := redis_pool.Get()

	metric_data, err := redis_con.Do("ZRANGEBYSCORE",
		"archive:"+name, start, end)
	if err != nil {
		log.Println(err)
		return
	}
	md, ok := metric_data.([]interface{})
	if !ok {
		log.Println("not []interface{}")
		return
	}
	var kv []interface{}
	for _, v := range md {
		t_v := strings.Split(string(v.([]byte)), ":")
		if len(t_v) != 2 {
			log.Println("error redis data")
			continue
		}
		t, _ := strconv.ParseInt(t_v[0], 10, 64)
		v, _ := strconv.ParseFloat(t_v[1], 64)
		kv = append(kv, []interface{}{t, v})
	}
	record_list[name] = kv

	w.Write(gen_json(record_list))
}

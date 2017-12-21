package gowordfinder

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
	"google.golang.org/appengine"
	"google.golang.org/appengine/memcache"
	"context"
)

const ascii_a = 97
const ascii_z = 122
const num_wordlist_files = 24

type result_struct struct {
	word_len  int
	num_found int
	compares  int
	words_arr []string
	cached    int
}

func init() {
	http.HandleFunc("/", root)
	http.HandleFunc("/find", find)
}

func root(httpw http.ResponseWriter, httpr *http.Request) {
	t, _ := template.ParseFiles("app/templates/index.html")
	t.Execute(httpw, "index.html")
}

func getFileData(ctx context.Context, ana_key_len int, c chan<- []string) {

	data_arr := []string{}
	skey := "wl_" + strconv.Itoa(ana_key_len)

	data_filename := "app/wordlists/" + skey + ".txt"
	dfile, err := ioutil.ReadFile(data_filename)
	if err == nil {
		item := &memcache.Item{
			Key:   skey,
			Value: []byte(dfile),
		}
		memcache.Add(ctx, item)
		data_arr = strings.Split(string(dfile), "*")
	}

	c <- data_arr
}

func workerFunc(ctx context.Context, ana_key_len int, letter_counts_arr [26]uint8, result_chan chan<- *result_struct) {

	skey := "wl_" + strconv.Itoa(ana_key_len)
	result := new(result_struct)
	result.word_len = ana_key_len
	result.cached = 0

	item, err := memcache.Get(ctx, skey)
	if err == nil {
		result.words_arr = strings.Split(string(item.Value), "*")
		result.cached = 1
	} else {
		wordsfromfile_chan := make(chan []string)
		go getFileData(ctx, ana_key_len, wordsfromfile_chan)
		result.words_arr = <-wordsfromfile_chan
	}

	result.num_found = len(result.words_arr)

	for i := range result.words_arr {
		ana_key_letter_counts_arr := letter_counts_arr
		word_length := len(result.words_arr[i])
		for j := 0; j < word_length; j++ {
			idx := uint8(result.words_arr[i][j]) - ascii_a
			result.compares++
			if ana_key_letter_counts_arr[idx] == 0 {
				result.words_arr[i] = ""
				result.num_found--
				break
			} else {
				ana_key_letter_counts_arr[idx] -= 1
			}
		}
	}
	result_chan <- result
}

func workerFuncWild(ctx context.Context, ana_key_len int, letter_counts_arr [26]uint8, wild_count int, result_chan chan<- *result_struct) {

	skey := "wl_" + strconv.Itoa(ana_key_len)
	result := new(result_struct)
	result.word_len = ana_key_len
	result.cached = 0

	item, err := memcache.Get(ctx, skey)
	if err == nil {
		result.words_arr = strings.Split(string(item.Value), "*")
		result.cached = 1
	} else {
		wordsfromfile_chan := make(chan []string)
		go getFileData(ctx, ana_key_len, wordsfromfile_chan)
		result.words_arr = <-wordsfromfile_chan
	}

	result.num_found = len(result.words_arr)

	if wild_count >= ana_key_len {
		result_chan <- result
		return
	}

	for i := range result.words_arr {
		ana_key_letter_counts_arr := letter_counts_arr
		word_length := len(result.words_arr[i])
		wild_avail := wild_count
		for j := 0; j < word_length; j++ {
			idx := uint8(result.words_arr[i][j]) - ascii_a
			result.compares++
			if ana_key_letter_counts_arr[idx] == 0 {
				if wild_avail == 0 {
					result.words_arr[i] = ""
					result.num_found--
					break
				} else {
					wild_avail--
				}
			} else {
				ana_key_letter_counts_arr[idx] -= 1
			}
		}
	}
	
	result_chan <- result
}

/////
func outputHTML(httpw http.ResponseWriter, word_len int, words_arr []string) {
	fmt.Fprintf(httpw, "<p>"+strconv.Itoa(word_len)+" Letter Words</div><div class='wordcontainer'>")
	for i := range words_arr {
		if words_arr[i] != "" {
			fmt.Fprintf(httpw, "<div>"+words_arr[i]+"</div>")
		}
	}
	fmt.Fprintf(httpw, "</div>")
}

/////
func find(httpw http.ResponseWriter, httpr *http.Request) {
	var mem1, mem2 runtime.MemStats
	runtime.ReadMemStats(&mem1)

	runtime.GOMAXPROCS(runtime.NumCPU())

	httpw.Header().Set("Access-Control-Allow-Origin", "*")

	letters := strings.ToLower(httpr.FormValue("tray"))

	var letter_counts_arr [26]uint8
	len_letters := 0

	for i := 0; i < len(letters); i++ {
		if uint8(letters[i]) >= ascii_a && uint8(letters[i]) <= ascii_z {
			letter_counts_arr[uint8(letters[i])-ascii_a]++
			len_letters++
		}
	}

	wild_count, err := strconv.Atoi(httpr.FormValue("wc"))
	if err != nil || wild_count < 0 {
		wild_count = 0
	}

	if len_letters+wild_count < 2 {
		s := "Format: gowordfinder.appspot.com/find?tray=test&rt=[html,json]&wc=[0+]"
		fmt.Fprintf(httpw, s)
		return
	}

	return_type := httpr.FormValue("rt")
	if !(return_type == "html" || return_type == "json") {
		return_type = "html"
	}

	arr_results_arr := make([][]string, len_letters+wild_count-1)
	results_chan := make(chan *result_struct, len_letters+wild_count-1)

	numworkers := len_letters + wild_count - 1 //runtime.NumCPU()
	ana_key_len := len_letters + wild_count
	if ana_key_len > num_wordlist_files {
		ana_key_len = num_wordlist_files
	}
	workercount := numworkers
	start_time := time.Now()
	total_found := 0
	total_compares := 0
	total_cached := 0

	ctx := appengine.NewContext(httpr)

	for {
		for {
			if wild_count == 0 {
				go workerFunc(ctx, ana_key_len, letter_counts_arr, results_chan)
			} else {
				go workerFuncWild(ctx, ana_key_len, letter_counts_arr, wild_count, results_chan)
			}

			ana_key_len--
			workercount--
			if ana_key_len == 1 || workercount == 0 {
				break
			}
		}

		for {
			result := <-results_chan
			if result.num_found > 0 {
				arr_results_arr[len_letters+wild_count-result.word_len] = result.words_arr
				total_found += result.num_found
				total_compares += result.compares
				total_cached += result.cached
			}
			workercount++
			if workercount == numworkers {
				break
			}
		}

		if ana_key_len == 1 {
			break
		}
	}

	if return_type == "json" {

		json_arr := [][]string{}
		for i, j := 0, len(arr_results_arr); i < j; i++ {

			len_words_arr := len(arr_results_arr[i])
			if len_words_arr > 0 {
				temp_arr := make([]string, 0, len_words_arr)

				for m, n := 0, len_words_arr; m < n; m++ {
					if arr_results_arr[i][m] != "" {
						temp_arr = append(temp_arr, arr_results_arr[i][m])
					}
				}
				if len(temp_arr) > 0 {
					json_arr = append(json_arr, temp_arr)
				}
			}
		}

		b, _ := json.Marshal(json_arr)
		httpw.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(httpw, string(b))

	} else {

		result := "<h5 id='resultsFor'>Results for <span>"
		result += letters
		for i := 0; i < wild_count; i++ {
			result += "?"
		}
		result += "</span></h5>"
		fmt.Fprintf(httpw, result);
		for i := range arr_results_arr {
			if len(arr_results_arr[i]) > 0 {
				outputHTML(httpw, len_letters+wild_count-i, arr_results_arr[i])
			}
		}

		if total_found == 0 {
			fmt.Fprintf(httpw, "<p class='noresults'>No words found.</p>")
		}

		runtime.ReadMemStats(&mem2)

		fmt.Fprintf(httpw, "<p id='results_footer'>" + runtime.Version()+
			"<br />Compares: %d. Results: %d in %v. Memcached: %d<br />Memory: Alloc %d TotalAlloc %d HeapAlloc %d HeapSys %d</p>",
			total_compares, total_found, time.Since(start_time), total_cached,
			mem2.Alloc-mem1.Alloc, mem2.TotalAlloc-mem1.TotalAlloc, mem2.HeapAlloc-mem1.HeapAlloc, mem2.HeapSys-mem1.HeapSys)
	}
}

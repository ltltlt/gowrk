package gowrk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"text/tabwriter"
	"time"
)

func countBytes(reader io.Reader) (int64, error) {
	var count int64

	buffer := make([]byte, 1024)

	for {
		n, err := reader.Read(buffer)
		switch err {
		case nil:
			fallthrough
		case io.EOF:
			count += int64(n)
			return count, nil
		default:
			return 0, err
		}
	}
}

func calcMax(a, b time.Duration) time.Duration {
	if a < b {
		return b
	}
	return a
}

func calcMin(a, b time.Duration) time.Duration {
	if a > b {
		return b
	}
	return a
}

type request struct {
	id     int
	url    string
	method string
	body   io.Reader
}

type result struct {
	id         int
	size       int64
	statusCode int
	duration   time.Duration
	err        error
	url        string
	threadID   int
}

type Wrk struct {
	client   *http.Client
	requests chan *request
	results  chan *result
}

func (w *Wrk) sendRequest(request *request) *result {
	method := "GET"
	if request.method != "" {
		method = request.method
	}
	if request.method == "" && request.body != nil {
		method = "POST"
	}
	result := &result{}
	req, err := http.NewRequest(method, request.url, request.body)
	if err != nil {
		log.Panic(err)
	}
	// TODO: cookie support
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		result.err = err
		return result
	}
	defer resp.Body.Close()

	size, err := countBytes(resp.Body)
	if err != nil {
		result.err = err
		return result
	}

	result.statusCode = resp.StatusCode
	result.size = size

	return result
}

func printMap(arr []map[string]interface{}, writer io.Writer) {
	to := tabwriter.NewWriter(writer, 0, 0, 3, ' ', tabwriter.TabIndent)
	for _, table := range arr {
		for key, value := range table {
			fmt.Fprintf(to, "%s:\t%v\n", key, value)
		}
	}
	to.Flush()
}

func Start(targetURL string, c, n int, unique bool, dump, file string) {
	var dumpWriter *tabwriter.Writer

	if dump != "" {
		dumpFile, err := os.OpenFile(dump, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
		if err != nil {
			log.Panic(err)
		}
		dumpWriter = tabwriter.NewWriter(dumpFile, 0, 0, 3, ' ', tabwriter.TabIndent)
		if err != nil {
			log.Fatal(err)
		}

		defer func() {
			dumpWriter.Flush()
			dumpFile.Close()
		}()
	}

	wrk := &Wrk{
		client:   &http.Client{},
		requests: make(chan *request, 1),
		results:  make(chan *result, c),
	}

	var wg sync.WaitGroup

	for i := 0; i < c; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for request := range wrk.requests {
				start := time.Now()
				result := wrk.sendRequest(request)
				result.duration = time.Since(start)
				result.id = request.id
				result.url = request.url
				result.threadID = id
				wrk.results <- result
			}
		}(i)
	}

	go func() {
		wg.Wait()
		close(wrk.results)
	}()

	var avgDuration int64
	var avgSize int64
	var errors int64
	var totalTime time.Duration
	var max time.Duration
	var min time.Duration

	var ch = make(chan struct{})
	go func() {
		defer func() {
			ch <- struct{}{}
		}()
		consumeResults(wrk.results, dumpWriter)
	}()

	go func() {
		err := produceRequest(targetURL, file, n, unique, wrk.requests)
		if err != nil {
			log.Panic(err)
		}
		close(wrk.requests)
	}()

	<-ch

	var output bytes.Buffer
	printMap([]map[string]interface{}{
		map[string]interface{}{"Concurrent": c},
		map[string]interface{}{"Request": n},
		map[string]interface{}{"URL": targetURL},
		map[string]interface{}{"Total time": totalTime},
		map[string]interface{}{"Min Duration": min},
		map[string]interface{}{"Max Duration": max},
		map[string]interface{}{"Average Duration": time.Duration(avgDuration)},
		map[string]interface{}{"Average Size": fmt.Sprintf("%d bytes", avgSize)},
		map[string]interface{}{"Errors": errors},
	}, &output)

	log.Println(string(output.Bytes()))
}

func produceRequest(targetURL, file string, n int, unique bool, reqChan chan<- *request) error {
	var requests []*request
	if _, err := os.Stat(file); os.IsExist(err) {
		log.Printf("Read request from file: %s", file)
		reqMaps, err := readJSONFile(file)
		if err != nil {
			return err
		}
		for i, req := range reqMaps {
			requests = append(requests, &request{
				id:     i,
				url:    req["url"],
				method: req["method"],
				body:   strings.NewReader(req["body"]),
			})
		}
	} else {
		for i := 0; i < n; i++ {
			requests = append(requests, &request{
				id:  i,
				url: targetURL,
			})
		}
	}

	if unique {
		if err := setUnique(requests); err != nil {
			return err
		}
	}

	for _, req := range requests {
		reqChan <- req
	}

	log.Printf("\rFinished sending requests\n\n")
	return nil
}

func readJSONFile(file string) ([]map[string]string, error) {
	reader, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(reader)
	var result []map[string]string
	err = decoder.Decode(result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func setUnique(requests []*request) error {
	for _, req := range requests {
		url, err := url.Parse(req.url)
		if err != nil {
			return err
		}
		query := url.Query()
		query.Set("__", fmt.Sprintf("%d", time.Now().UnixNano()))
		url.RawQuery = query.Encode()
		req.url = url.String()
	}
	return nil
}

// stat store some statistics data
type stat struct {
	avgDuration              time.Duration
	avgSize                  int64
	count                    int
	nerror                   int
	totalSize                int64
	totalDuration            time.Duration // avgDuration*count
	totalTime                time.Duration // consume result cost time
	minDuration, maxDuration time.Duration
}

func consumeResults(resChan <-chan *result, dumpWriter io.Writer) *stat {
	if dumpWriter != nil {
		fmt.Fprintf(dumpWriter, "%s\n",
			strings.Join([]string{
				"id",
				"NThread",
				"Duration",
				"Size",
				"Status",
				"Code",
				"Error",
			}, ",\t"))
	}

	var (
		nerror        int
		totalSize     int64
		count         int
		totalDuration time.Duration
		min, max      time.Duration

		start        = time.Now()
		errorMessage string
	)
	// maybe we need sort results by id
	for result := range resChan {
		errorMessage = ""
		if result.err != nil {
			nerror++
			errorMessage = result.err.Error()
		} else {
			if min == 0 {
				min = result.duration
			}

			max = calcMax(max, result.duration)
			min = calcMin(min, result.duration)
			totalDuration += result.duration
			totalSize += result.size
			count++
		}

		if dumpWriter != nil {
			fmt.Fprintf(
				dumpWriter,
				"%d,\t%d,\t%s,\t%d,\t%d,\t%s\n",
				result.id,
				result.threadID,
				result.duration,
				result.size,
				result.statusCode,
				errorMessage)
		}
	}

	var avgDuration time.Duration
	var avgSize int64
	if count > 0 {
		avgDuration = totalDuration / time.Duration(count)
		avgSize = totalSize / int64(count)
	}

	totalTime := time.Since(start)
	return &stat{
		avgDuration:   avgDuration,
		avgSize:       avgSize,
		count:         count,
		nerror:        nerror,
		totalSize:     totalSize,
		totalTime:     totalTime,
		totalDuration: totalDuration,
		minDuration:   min,
		maxDuration:   max,
	}
}

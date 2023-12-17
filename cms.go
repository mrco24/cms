package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
)

var (
	apiURL        = "https://whatcms.org/API/Tech"
	apiKey        = "wr9c8mpevbl4ttaau1vysysq3a92zhlhqo17k5f7xwp7t7jm7682sct6xf71znre8lkvke" // Replace with your actual API key
	output        = ""
	cmsFilter     = ""
	allCMS        = false
	subdomainFile = ""
	threads       = 10
)

var wg sync.WaitGroup

// Result represents the result of CMS check for a subdomain
type Result struct {
	Subdomain string
	CMSName   string
	Version   string
	Error     error
}

func init() {
	flag.StringVar(&output, "o", "", "Output file path")
	flag.StringVar(&cmsFilter, "i", "", "Filter by CMS name or version")
	flag.BoolVar(&allCMS, "a", false, "Show all CMS versions")
	flag.StringVar(&subdomainFile, "f", "", "Path to the subdomain list file")
	flag.IntVar(&threads, "t", 10, "Number of concurrent threads (goroutines)")
}

func checkCMS(subdomain string, resultChan chan<- Result) {
	defer wg.Done()

	client := resty.New()

	url := fmt.Sprintf("%s?key=%s&url=%s", apiURL, apiKey, subdomain)
	resp, err := client.R().Get(url)

	result := Result{
		Subdomain: subdomain,
	}

	if err != nil {
		result.Error = fmt.Errorf("Error making request: %v", err)
		resultChan <- result
		return
	}

	// Parse the response JSON or handle accordingly based on the API structure
	var apiResult map[string]interface{}
	if err := json.Unmarshal([]byte(resp.String()), &apiResult); err != nil {
		result.Error = fmt.Errorf("Error parsing JSON: %v", err)
		resultChan <- result
		return
	}

	// Check if the API request was successful
	if code, ok := apiResult["result"].(map[string]interface{})["code"].(float64); ok && code == 200 {
		// Extract CMS information from the results array
		results, ok := apiResult["results"].([]interface{})
		if !ok {
			result.Error = fmt.Errorf("Invalid structure in the API response")
			resultChan <- result
			return
		}

		for _, item := range results {
			if cms, ok := item.(map[string]interface{}); ok {
				name, _ := cms["name"].(string)
				version, _ := cms["version"].(string)

				// Check if CMS name matches the filter when the -i flag is provided
				if cmsFilter == "" || strings.EqualFold(name, cmsFilter) {
					result.CMSName = name
					result.Version = version
					resultChan <- result

					if output != "" {
						writeToFile(subdomain, fmt.Sprintf("CMS Name: %s, Version: %s", name, version))
					}
				}
			}
		}
	} else {
		// Handle the case where the API request was not successful
		result.Error = fmt.Errorf("API request was not successful. Code: %v, Message: %v",
			apiResult["result"].(map[string]interface{})["code"], apiResult["result"].(map[string]interface{})["msg"])

		resultChan <- result

		// If rate-limited, introduce a delay before the next request
		if code == 120 {
			retryInSeconds, _ := apiResult["retry_in_seconds"].(float64)
			fmt.Printf("Retrying in %f seconds...\n", retryInSeconds)
			time.Sleep(time.Duration(retryInSeconds) * time.Second)
			checkCMS(subdomain, resultChan) // Retry the request
		}
	}
}

func writeToFile(subdomain, cmsInfo string) {
	f, err := os.OpenFile(output, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("Error opening output file: %v\n", err)
		return
	}
	defer f.Close()

	_, err = f.WriteString(fmt.Sprintf("Subdomain: %s, %s\n", subdomain, cmsInfo))
	if err != nil {
		fmt.Printf("Error writing to output file: %v\n", err)
	}
}

func main() {
	flag.Parse()

	if subdomainFile == "" {
		fmt.Println("Error: Subdomain list file path is required.")
		return
	}

	file, err := os.Open(subdomainFile)
	if err != nil {
		fmt.Printf("Error opening file: %v\n", err)
		return
	}
	defer file.Close()

	resultChan := make(chan Result, threads)

	// Start a goroutine to collect results and print them
	go func() {
		for {
			select {
			case result, ok := <-resultChan:
				if !ok {
					return
				}

				if result.Error != nil {
					fmt.Printf("Error for subdomain %s: %v\n", result.Subdomain, result.Error)
				} else {
					fmt.Printf("Subdomain: %s, CMS Name: %s, Version: %s\n", result.Subdomain, result.CMSName, result.Version)
				}
			}
		}
	}()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		subdomain := strings.TrimSpace(scanner.Text())
		fmt.Printf("Processing subdomain: %s\n", subdomain)

		wg.Add(1)
		go checkCMS(subdomain, resultChan)
	}

	wg.Wait()
	close(resultChan)

	if err := scanner.Err(); err != nil {
		fmt.Printf("Error reading file: %v\n", err)
	}
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "pulltime",
	Short: "A CLI tool to measure image pull time from remote registries",
}

var imageCmd = &cobra.Command{
	Use:   "image [IMAGE_URL]",
	Short: "Measure pull time for a container image",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		imageURL := args[0]
		fmt.Printf("Pulling image: %s\n", imageURL)
		start := time.Now()
		pullCmd := fmt.Sprintf("docker pull %s", imageURL)
		output, err := runCommand(pullCmd)
		if err != nil {
			fmt.Printf("Error pulling image: %v\n", err)
			os.Exit(1)
		}
		elapsed := time.Since(start)
		fmt.Printf("Image pull completed in: %v\n", elapsed)
		fmt.Println("--- Docker Output ---")
		fmt.Println(output)
	},
}

var (
	concurrency   int
	timeoutSec    int
	outputSummary bool
)

var benchmarkCmd = &cobra.Command{
	Use:   "benchmark [IMAGE_URLS...]",
	Short: "Benchmark pull times for multiple container images and output JSON report",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var resultsMu sync.Mutex
		results := make([]Result, 0, len(args))
		wg := sync.WaitGroup{}
		sem := make(chan struct{}, concurrency)
		for _, imageURL := range args {
			wg.Add(1)
			go func(imageURL string) {
				defer wg.Done()
				sem <- struct{}{}
				ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
				defer cancel()
				start := time.Now()
				pullCmd := fmt.Sprintf("docker pull %s", imageURL)
				cmd := exec.CommandContext(ctx, "bash", "-c", pullCmd)
				output, err := cmd.CombinedOutput()
				elapsed := time.Since(start)
				registry := parseRegistry(imageURL)
				res := Result{
					Image:      imageURL,
					Registry:   registry,
					Success:    err == nil && ctx.Err() == nil,
					PullTimeMs: elapsed.Milliseconds(),
					StartTime:  start.Format(time.RFC3339),
					EndTime:    time.Now().Format(time.RFC3339),
					CmdOutput:  string(output),
				}
				if err != nil {
					res.Error = err.Error()
				}
				if ctx.Err() != nil {
					res.Error = ctx.Err().Error()
				}
				// Parse output for more details (layers, bytes)
				parseDockerOutput(&res, string(output))
				resultsMu.Lock()
				results = append(results, res)
				resultsMu.Unlock()
				<-sem
			}(imageURL)
		}
		wg.Wait()
		if outputSummary {
			printSummary(results)
		}
		jsonOut, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			fmt.Printf("Error generating JSON: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(jsonOut))
	},
}

var compareCmd = &cobra.Command{
	Use:   "compare [IMAGE_MIRROR] [IMAGE_REMOTE]",
	Short: "Compare pull times between a mirror and a remote registry, outputting a JSON report",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		type CompareResult struct {
			Image      string `json:"image"`
			Registry   string `json:"registry"`
			PullTimeMs int64  `json:"pull_time_ms"`
			Success    bool   `json:"success"`
			Error      string `json:"error,omitempty"`
		}
		images := []string{args[0], args[1]}
		var results []CompareResult
		for _, imageURL := range images {
			start := time.Now()
			pullCmd := fmt.Sprintf("docker pull %s", imageURL)
			_, err := runCommand(pullCmd)
			elapsed := time.Since(start)
			registry := parseRegistry(imageURL)
			res := CompareResult{
				Image:      imageURL,
				Registry:   registry,
				PullTimeMs: elapsed.Milliseconds(),
				Success:    err == nil,
			}
			if err != nil {
				res.Error = err.Error()
			}
			results = append(results, res)
		}
		jsonOut, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			fmt.Printf("Error generating JSON: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(jsonOut))
	},
}

var ciCmd = &cobra.Command{
	Use:   "ci [IMAGE_URL] [--output <file>]",
	Short: "Measure and export image pull time for CI/CD integration (JSON output)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		imageURL := args[0]
		start := time.Now()
		pullCmd := fmt.Sprintf("docker pull %s", imageURL)
		_, err := runCommand(pullCmd)
		elapsed := time.Since(start)
		result := struct {
			Image     string `json:"image"`
			Registry  string `json:"registry"`
			Success   bool   `json:"success"`
			PullTime  int64  `json:"pull_time_ms"`
			Error     string `json:"error,omitempty"`
			Timestamp string `json:"timestamp"`
		}{
			Image:     imageURL,
			Registry:  parseRegistry(imageURL),
			Success:   err == nil,
			PullTime:  elapsed.Milliseconds(),
			Timestamp: time.Now().Format(time.RFC3339),
		}
		if err != nil {
			result.Error = err.Error()
		}
		jsonOut, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Printf("Error generating JSON: %v\n", err)
			os.Exit(1)
		}
		outputFile, _ := cmd.Flags().GetString("output")
		if outputFile != "" {
			err := os.WriteFile(outputFile, jsonOut, 0644)
			if err != nil {
				fmt.Printf("Error writing to file: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Results written to %s\n", outputFile)
		} else {
			fmt.Println(string(jsonOut))
		}
	},
}

var warmupCmd = &cobra.Command{
	Use:   "warmup [IMAGE_URL]",
	Short: "Repeatedly pull and remove an image to measure cold and warm cache pull times",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		imageURL := args[0]
		iterations, _ := cmd.Flags().GetInt("iterations")
		delay, _ := cmd.Flags().GetInt("delay")
		var results []struct {
			Iteration  int    `json:"iteration"`
			PullTimeMs int64  `json:"pull_time_ms"`
			CacheState string `json:"cache_state"`
			Success    bool   `json:"success"`
			Error      string `json:"error,omitempty"`
		}
		for i := 1; i <= iterations; i++ {
			// Remove image before cold pull
			if i == 1 {
				exec.Command("bash", "-c", fmt.Sprintf("docker rmi -f %s", imageURL)).Run()
			}
			start := time.Now()
			_, err := runCommand(fmt.Sprintf("docker pull %s", imageURL))
			elapsed := time.Since(start)
			cacheState := "cold"
			if i > 1 {
				cacheState = "warm"
			}
			res := struct {
				Iteration  int    `json:"iteration"`
				PullTimeMs int64  `json:"pull_time_ms"`
				CacheState string `json:"cache_state"`
				Success    bool   `json:"success"`
				Error      string `json:"error,omitempty"`
			}{
				Iteration:  i,
				PullTimeMs: elapsed.Milliseconds(),
				CacheState: cacheState,
				Success:    err == nil,
			}
			if err != nil {
				res.Error = err.Error()
			}
			results = append(results, res)
			if i < iterations {
				// Remove image for next run
				exec.Command("bash", "-c", fmt.Sprintf("docker rmi -f %s", imageURL)).Run()
				time.Sleep(time.Duration(delay) * time.Millisecond)
			}
		}
		jsonOut, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			fmt.Printf("Error generating JSON: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(jsonOut))
	},
}

type Result struct {
	Image      string `json:"image"`
	Registry   string `json:"registry"`
	Success    bool   `json:"success"`
	PullTimeMs int64  `json:"pull_time_ms"`
	StartTime  string `json:"start_time"`
	EndTime    string `json:"end_time"`
	Error      string `json:"error,omitempty"`
	Bytes      int64  `json:"bytes_downloaded,omitempty"`
	Layers     int    `json:"layers,omitempty"`
	CmdOutput  string `json:"cmd_output,omitempty"`
}

func runCommand(cmd string) (string, error) {
	c := exec.Command("bash", "-c", cmd)
	output, err := c.CombinedOutput()
	return string(output), err
}

func parseRegistry(image string) string {
	if idx := indexOf(image, '/'); idx > 0 && !isOfficialDockerHub(image) {
		return image[:idx]
	}
	return "docker.io"
}

func indexOf(s string, c rune) int {
	for i, ch := range s {
		if ch == c {
			return i
		}
	}
	return -1
}

func isOfficialDockerHub(image string) bool {
	return indexOf(image, '.') == -1 && indexOf(image, ':') == -1
}

func parseDockerOutput(res *Result, output string) {
	var layers, bytes int64
	for _, line := range splitLines(output) {
		if n, _ := fmt.Sscanf(line, "Downloaded newer image for %*s"); n > 0 {
			continue
		}
		if n, _ := fmt.Sscanf(line, "%dB", &bytes); n == 1 {
			res.Bytes = bytes
		}
		if n, _ := fmt.Sscanf(line, "Pulling fs layer"); n > 0 {
			layers++
		}
	}
	if layers > 0 {
		res.Layers = int(layers)
	}
}

func splitLines(s string) []string {
	return strings.Split(s, "\n")
}

func printSummary(results []Result) {
	total := len(results)
	success := 0
	var min, max, sum int64
	for i, r := range results {
		if r.Success {
			success++
			if i == 0 || r.PullTimeMs < min {
				min = r.PullTimeMs
			}
			if r.PullTimeMs > max {
				max = r.PullTimeMs
			}
			sum += r.PullTimeMs
		}
	}
	fmt.Printf("\nSummary: %d/%d succeeded | min: %dms | max: %dms | avg: %.2fms\n", success, total, min, max, float64(sum)/float64(success))
}

func init() {
	benchmarkCmd.Flags().IntVarP(&concurrency, "concurrent", "c", 2, "Number of concurrent pulls")
	benchmarkCmd.Flags().IntVarP(&timeoutSec, "timeout", "t", 120, "Timeout (seconds) for each pull")
	benchmarkCmd.Flags().BoolVarP(&outputSummary, "summary", "s", false, "Print summary statistics")
	rootCmd.AddCommand(imageCmd)
	rootCmd.AddCommand(benchmarkCmd)
	rootCmd.AddCommand(compareCmd)
	rootCmd.AddCommand(ciCmd)
	warmupCmd.Flags().IntP("iterations", "n", 3, "Number of pull/remove iterations")
	warmupCmd.Flags().IntP("delay", "d", 1000, "Delay (ms) between iterations")
	rootCmd.AddCommand(warmupCmd)
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

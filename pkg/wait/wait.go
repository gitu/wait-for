package wait

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"sync"
	"time"
)

type Wait struct {
	HostPorts      []string
	Urls           []string
	StatusInterval time.Duration
	CheckInterval  time.Duration
	GlobalTimeout  time.Duration
	CheckTimeout   time.Duration
}

func (w *Wait) Wait() error {
	urlStatuses, err := newUrlStatus(w.Urls)
	if err != nil {
		return err
	}
	hostPorts, err := parseHostPorts(w.HostPorts)
	if err != nil {
		return err
	}

	checkers := make([]*WrappedChecker, 0, len(hostPorts)+len(urlStatuses))
	for _, hp := range hostPorts {
		checkers = append(checkers, newWrappedChecker(hp, w.CheckInterval, w.CheckTimeout))
	}
	for _, u := range urlStatuses {
		checkers = append(checkers, newWrappedChecker(u, w.CheckInterval, w.CheckTimeout))
	}

	ctx, cancel := context.WithTimeout(context.Background(), w.GlobalTimeout)
	defer cancel()

	fmt.Printf("Waiting for %d targets\n", len(checkers))
	for _, c := range checkers {
		fmt.Printf("👀  %s\n", c.Name())
	}
	fmt.Println("####################")

	wg := sync.WaitGroup{}
	go statusLogger(ctx, w.StatusInterval, checkers)
	wg.Add(len(checkers))
	for _, c := range checkers {
		go c.Start(ctx, &wg)
	}
	wg.Wait()

	errors := 0
	for _, c := range checkers {
		if c.LastStatus.Error != nil {
			errors++
		}
	}

	fmt.Println("####################")
	fmt.Printf("Finished waiting for %d targets\n", len(checkers))
	for _, c := range checkers {
		if c.LastStatus.Healthy {
			fmt.Printf("✔️  %s -- %v\n", c.LastStatus.Target, c.Duration)
		} else {
			fmt.Printf("❌  %s -- %v -- ERROR: %v\n", c.LastStatus.Target, c.Duration, c.LastStatus.Error)
		}
	}
	fmt.Println("####################")
	if errors == 1 {
		return fmt.Errorf("%d target unhealthy", errors)
	} else if errors > 1 {
		return fmt.Errorf("%d targets unhealthy", errors)
	}
	return nil
}

func newWrappedChecker(c Checker, interval time.Duration, timeout time.Duration) *WrappedChecker {
	return &WrappedChecker{
		Checker:       c,
		CheckInterval: interval,
		LastStatus:    &StatusInfo{Target: c.Name(), Healthy: false, Error: nil},
		Timeout:       timeout,
	}
}

func statusLogger(ctx context.Context, interval time.Duration, i []*WrappedChecker) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fmt.Printf("Status: %v\n", time.Now().Format(time.DateTime))
			for _, c := range i {
				if c.LastStatus.Healthy {
					fmt.Printf("✔️  %s -- %v\n", c.LastStatus.Target, c.Duration)
				} else {
					fmt.Printf("⚠️  %s -- %v -- ERROR: %v\n", c.LastStatus.Target, c.Duration, c.LastStatus.Error)
				}
			}
		}
	}
}

type WrappedChecker struct {
	Checker
	CheckInterval time.Duration
	LastStatus    *StatusInfo
	Duration      time.Duration
	Timeout       time.Duration
}

func (w *WrappedChecker) Start(ctx context.Context, group *sync.WaitGroup) {
	defer group.Done()
	start := time.Now()
	w.LastStatus = &StatusInfo{
		Target:  w.Name(),
		Healthy: false,
		Error:   nil,
	}
	for {
		select {
		case <-ctx.Done():
			if w.LastStatus.Error == nil {
				w.LastStatus.Error = ctx.Err()
			}
			w.Duration = time.Since(start)
			return
		default:
			ctx, cancel := context.WithTimeout(ctx, w.Timeout)
			ok, err := w.Check(ctx)
			cancel()
			w.Duration = time.Since(start)
			if err != nil {
				w.LastStatus.Error = err
			} else {
				w.LastStatus.Error = nil
				w.LastStatus.Healthy = ok
			}
			if w.LastStatus.Healthy {
				return
			}
			time.Sleep(w.CheckInterval)
		}
	}
}

func parseHostPorts(ports []string) ([]*hostPort, error) {
	var hostPorts []*hostPort
	for _, p := range ports {
		host, port, err := net.SplitHostPort(p)
		if err != nil {
			return nil, fmt.Errorf("invalid host:port %s (%v)", p, err)
		}
		portnum, err := strconv.Atoi(port)
		if err != nil {
			return nil, fmt.Errorf("invalid port %s (%v)", port, err)
		}
		hostPorts = append(hostPorts, &hostPort{host: host, port: portnum})
	}
	return hostPorts, nil
}

type StatusInfo struct {
	Target  string
	Healthy bool
	Error   error
}

type Checker interface {
	Check(ctx context.Context) (bool, error)
	Name() string
}

type urlStatus struct {
	url    string
	status int
}

func newUrlStatus(urls []string) ([]*urlStatus, error) {
	urlStatuses, err := parseUrls(urls)
	if err != nil {
		return nil, err
	}
	var result []*urlStatus
	for u, s := range urlStatuses {
		result = append(result, &urlStatus{url: u, status: s})
	}
	return result, nil
}
func (u *urlStatus) Check(ctx context.Context) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.url, nil)
	if err != nil {
		return false, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	if res.StatusCode == u.status {
		return true, nil
	}
	return false, fmt.Errorf("expected status code %d, got %d", u.status, res.StatusCode)
}
func (u *urlStatus) Name() string {
	return u.url
}

type hostPort struct {
	host string
	port int
}

func (h *hostPort) Name() string {
	return fmt.Sprintf("%s:%d", h.host, h.port)
}

func (h *hostPort) Check(ctx context.Context) (bool, error) {
	addr := fmt.Sprintf("%s:%d", h.host, h.port)
	deadline, ok := ctx.Deadline()
	if !ok {
		return false, fmt.Errorf("no deadline set")
	}
	conn, err := net.DialTimeout("tcp", addr, deadline.Sub(time.Now()))
	if err != nil {
		return false, err
	}
	defer conn.Close()
	return true, nil
}

func parseUrls(urls []string) (map[string]int, error) {
	hasStatusCode := regexp.MustCompile(`^(\d{3}):(https?://.*)$`)
	urlStatuses := make(map[string]int)
	for _, u := range urls {
		if hasStatusCode.MatchString(u) {
			statusCode := hasStatusCode.FindStringSubmatch(u)[1]
			u = hasStatusCode.FindStringSubmatch(u)[2]
			var err error
			urlStatuses[u], err = strconv.Atoi(statusCode)
			if err != nil {
				return nil, fmt.Errorf("invalid status code: %s (%v)", statusCode, err)
			}
			continue
		} else {
			urlStatuses[u] = 200
		}

		_, err := url.ParseRequestURI(u)
		if err != nil {
			return nil, fmt.Errorf("invalid url: %s (%v)", u, err)
		}

	}
	return urlStatuses, nil
}

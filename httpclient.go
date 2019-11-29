package main

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

const (
	uaList    = "https://raw.githubusercontent.com/cvandeplas/pystemon/master/user-agents.txt"
	defaultUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13_5) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/67.0.3396.87 Safari/537.36"
)

type httpClient struct {
	Client    *http.Client
	UserAgent string
}

func newHTTPClient() *httpClient {
	client := &http.Client{}
	client.Jar, _ = cookiejar.New(nil)
	userAgent := defaultUA

	resp, err := http.Get(uaList)
	if err == nil {
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)

		if err == nil {
			userAgentParsed := strings.Split(string(body), "\n")
			if len(userAgentParsed) > 0 {
				userAgent = userAgentParsed[rand.Intn(len(userAgentParsed))]
			}
		}
	}

	logInfo("User-Agent: " + userAgent)

	return &httpClient {
		Client:    client,
		UserAgent: userAgent,
	}
}

func (c *httpClient) Get(url string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("User-Agent", c.UserAgent)

	res, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	return res, err
}

// Taken from https://godoc.org/golang.org/x/net/html#example-Parse
func loopFindToken(n *html.Node) string {
	if n.Type == html.ElementNode && n.Data == "input" {
		var exists bool 
		var token string

		for _, data := range n.Attr {
			if data.Val == "login_form__token" {
				exists = true
			} else if data.Key == "value" {
				token = data.Val
			}
		}

		if exists == true && token != "" {
			return token
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if token := loopFindToken(c); token != "" {
			return token
		}
	}
	return ""
}

func (c *httpClient) Login(user, pass string) error {
	resp, err := http.Get("https://www.crunchyroll.com/login")
	if err != nil {
		return fmt.Errorf("getting login page: %w", err)
	}

	defer resp.Body.Close()
	nodes, err := html.Parse(resp.Body)
	if err != nil {
		return fmt.Errorf("parsing login response: %w", err)
	}

	token := loopFindToken(nodes)
	if token == "" {
		return fmt.Errorf("unable to find token")
	}

	body := url.Values {
		"login_form[name]":         {user},
		"login_form[password]":     {pass},
		"login_form[redirect_url]": {"/"},
		"login_form[_token]":       {token},
	}

	if _, err := c.Client.PostForm("https://www.crunchyroll.com/login", body); err != nil {
		return fmt.Errorf("posting authentication request")
	}
// Re-implement this
/*
	if resp, err := c.Get("http://www.crunchyroll.com/"); err == nil {
		checkDoc, err := goquery.NewDocumentFromResponse(resp)
		if err != nil {
			return errors.New("Failed to parse session validation page: " + err.Error())
		}

		if resp.StatusCode == 200 && strings.TrimSpace(checkDoc.Find("li.username").First().Text()) != "" {
			return nil
		}
		return errors.New("Failed to validate login")
	}
	return errors.New("Failed to get crunchyroll website")
*/
	return nil
}

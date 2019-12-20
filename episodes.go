package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

type crEpisode struct {
	Title        string
	Number       string
	SeriesTitle  string
	SeasonNumber string
	EpisodeURL   string
	StreamURL    string
	//SubtitleURL  string
}

type configStruct struct {
	Streams []struct {
		Format    string `json:"format"`
		AudioLang string `json:"audio_lang"`
		SubLang   string `json:"hardsub_lang"`
		URL       string `json:"url"`
	} `json:"streams"`

	Subtitles []struct {
		Language string `json:"language"`
		URL      string `json:"url"`
		Format   string `json:"format"`
	} `json:"subtitles"`

	Metadata struct {
		Title string `json:"title"`
		//Number string `json:"episode_number"`
		Number string `json:"display_episode_number"`
	} `json:"metadata"`
}

type contextStruct struct {
	Season struct {
		Number string `json:"seasonNumber"`
	} `json:"partOfSeason"`

	Series struct {
		Title string `json:"name"`
	} `json:"partOfSeries"`
}

func getValues(node *html.Node, value, keyword string) (values []string, nodes []*html.Node) {
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			var exists bool
			var data string

			for _, attr := range n.Attr {
				if attr.Key == "class" && strings.Contains(attr.Val, keyword) {
					exists = true
				} else if attr.Key == value {
					data = attr.Val
				}

				if exists == true && data != "" {
					values = append(values, data)
					nodes = append(nodes, n)
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(node)
	return
}

func getEpisodes(client *httpClient, showURL string, dubbed bool) ([]*crEpisode, error) {
	submatches := regexp.MustCompile(crunchyrollReg).FindStringSubmatch(showURL)

	if len(submatches) != 3 {
		return nil, fmt.Errorf("invalid crunchyroll url %q", showURL)
	}

	episodes := []*crEpisode{}

	if submatches[2] == "" { // If there is no extra parameter after the slash, then it is a series.
		resp, err := client.Get(showURL)
		if err != nil {
			return nil, fmt.Errorf("getting series page: %w", err) 
		}

		defer resp.Body.Close()
		nodes, err := html.Parse(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("parsing series page: %w", err)
		}

		seasons, data := getValues(nodes, "title", "season-dropdown")
		var hrefs []string 

		if len(seasons) == 0 { //It's a single season and doesn't have the season dividers
			hrefs, _ = getValues(nodes, "href", "titlefix episode")			
		} else { //It's more than one season and does have them
			for i, name := range seasons {
				hasDubbedTitle := strings.Contains(name, "Dubbed")

				if (dubbed && hasDubbedTitle) || (!dubbed && !hasDubbedTitle) {
					//fmt.Println("doing: " + name)
					hrefs, _ = getValues(data[i].Parent, "href", "titlefix episode")				
				}
			}
		}

		for _, href := range hrefs {
			episodes = append(episodes, newEpisode("http://www.crunchyroll.com" + href))
		}
	} else {
		episodes = append(episodes, newEpisode(showURL))
	}

	for i := len(episodes)/2 - 1; i >= 0; i-- {
		o := len(episodes) - 1 - i
		episodes[i], episodes[o] = episodes[o], episodes[i]
	}
	return episodes, nil
}

func newEpisode(showURL string) *crEpisode {
	return &crEpisode{
		EpisodeURL: showURL,
	}
}

func (e *crEpisode) GetEpisodeInfo(client *httpClient, subLang string) error {
	subLang = strings.ReplaceAll(subLang, "-", "")
	if subLang == "none" {
		subLang = ""
	}

	res, err := client.Get(e.EpisodeURL)
	if err != nil {
		return fmt.Errorf("getting episode response: %w", err)
	}

	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("reading episode response: %w", err)
	}

	vilosResult := regexp.MustCompile(`vilos.config.media = (.*);\n`).FindSubmatch(body)
	if vilosResult == nil {
		return fmt.Errorf("parsing metadata regexp")
	}

	var config configStruct
	if err = json.Unmarshal(vilosResult[1], &config); err != nil {
		return fmt.Errorf("unmarshaling metadata regexp: %w", err)
	}

	// If this works it will return 4 integers, with the first two
	// Being the index length of the full expression and the last
	// Two being the length of the captured expression. We need the
	// `{"@context":\[` part of the full expression, so we take 
	// The first int and the fourth, which will get the beginning
	// Of the full expression but stop at the end of the matched
	// expression, which will allow us to parse the result to a struct
	contextResult := regexp.MustCompile(`{"@context":\[(.*)</script>`).FindSubmatchIndex(body)
	if contextResult == nil {
		return fmt.Errorf("parsing context regexp")
	}

	var context contextStruct
	if err = json.Unmarshal(body[contextResult[0]:contextResult[3]], &context); err != nil {
		return fmt.Errorf("unmarshaling context regexp: %w", err)
	}

	if context.Season.Number == "0" {
		context.Season.Number = "1"
	}

	e.Title = config.Metadata.Title
	e.Number = config.Metadata.Number
	e.SeriesTitle = context.Series.Title
	e.SeasonNumber = context.Season.Number

	// Two methods, hardsubs or no hardsubs
	for _, stream := range config.Streams {
		if stream.Format == "adaptive_hls" && stream.SubLang == subLang {
			e.StreamURL = stream.URL
			break
		}
	}

	if e.StreamURL == "" {
		return fmt.Errorf("could not find stream with language %q", subLang)
	}
	return nil
}

func (e *crEpisode) Download(client *httpClient, quality string, options bool) error {
	if val, exists := resolutionList[quality]; exists == true {
		quality = val
	}

	best, err := bestMasterStream(client, e.StreamURL, quality)
	if options {
		return optionsErr
	}

	if err != nil {
		return fmt.Errorf("getting best stream url: %w", err)
	}

	logInfo("Closest quality: %dx%d", best.Resolution.Width, best.Resolution.Height)
	downloader, err := newDownloader(client, "episode", best.URI, 15)
	if err != nil {
		return fmt.Errorf("creating hls downloader: %w", err)
	}

	if err = downloader.Download(true); err != nil {
		return fmt.Errorf("downloading stream: %w", err)
	}
	return nil
}

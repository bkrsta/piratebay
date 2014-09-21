package piratebay

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	ROOTURI        = `http://thepiratebay.org`
	INFRAURI       = `/search/a/0/99/0`
	CATEGORYREGEXP = `<opt.*? (.*?)="(.*?)">([A-Za-z- ()/]+)?<?`
	ORDERINGREGEXP = `/(\d+)/0" title="Order by (.*?)"`
	SEARCHURI      = `/search/%s/0/%s/%s`
	SEARCHREGEXP   = `(?s)category">(.*?)</a>.*?/browse/(\d+)".*?category">(.*?)</a>.*?torrent/(\d+)/.*?>(.*?)</a>.*?(magnet.*?)".*?(vip|11x11).*?Uploaded (.*?), Size (.*?), ULed by .*?>(.*?)<.*?right">(\d+)<.*?right">(\d+)</td>`
	INFOURI        = `/ajax_details_filelist.php?id=%s`
	INFOREGEXP     = `<td align="left">(.*?)</td>`
)

var (
	killHTMLRegexp = regexp.MustCompile(`<.*?>`)
)

type Category struct {
	Group string
	Title string
	ID    string
}

func (c *Category) String() string {
	return fmt.Sprintf("%s/%s", c.Group, c.Title)
}

type Ordering struct {
	Title string
	ID    string
}

func (o *Ordering) String() string {
	return fmt.Sprintf("%s", o.Title)
}

type File struct {
	Title string
	Size  int64
}

func (f *File) String() string {
	return fmt.Sprintf("%s", f.Title)
}

type Site struct {
	RootURI        string
	InfraURI       string
	SearchURI      string
	InfoURI        string
	CategoryREGEXP *regexp.Regexp
	OrderingREGEXP *regexp.Regexp
	SearchREGEXP   *regexp.Regexp
	InfoREGEXP     *regexp.Regexp
	Categories     map[string]map[string]string
	Orderings      map[string]string
	Client         *http.Client
	Logger         *log.Logger

	infraData string
}

func NewSite() *Site {
	return &Site{
		RootURI:        ROOTURI,
		InfraURI:       INFRAURI,
		SearchURI:      SEARCHURI,
		InfoURI:        INFOURI,
		CategoryREGEXP: regexp.MustCompile(CATEGORYREGEXP),
		OrderingREGEXP: regexp.MustCompile(ORDERINGREGEXP),
		SearchREGEXP:   regexp.MustCompile(SEARCHREGEXP),
		InfoREGEXP:     regexp.MustCompile(INFOREGEXP),
		Categories:     nil,
		Orderings:      nil,
		Client:         &http.Client{},
		Logger:         log.New(os.Stderr, "DEBUG: ", log.Lshortfile),
	}
}

func (s *Site) String() string {
	return fmt.Sprintf("%s", s.RootURI)
}

func (s *Site) getInfraData() (string, error) {
	if s.infraData != "" {
		return s.infraData, nil
	}
	res, err := s.Client.Get(s.RootURI + s.InfraURI)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("Unsuccessful request: %d", res.StatusCode)
	}
	data, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	s.infraData = string(data)
	return s.infraData, nil
}

func (s *Site) UpdateCategories() error {
	data, err := s.getInfraData()
	if err != nil {
		return err
	}
	s.parseCategories(data)
	return nil
}

func (s *Site) UpdateOrderings() error {
	data, err := s.getInfraData()
	if err != nil {
		return err
	}
	s.parseOrderings(data)
	return nil
}

func (s *Site) parseCategories(input string) {
	var group string
	s.Categories = make(map[string]map[string]string, 8)
	s.Categories[""] = make(map[string]string, 1)
	for _, match := range s.CategoryREGEXP.FindAllStringSubmatch(input, -1) {
		switch match[1] {
		case "label":
			group = strings.ToLower(match[2])
			if _, present := s.Categories[group]; !present {
				s.Categories[group] = make(map[string]string, 8)
			}
		case "value":
			category := strings.ToLower(match[3])
			s.Categories[group][category] = match[2]
		}
	}
	return
}

func (s *Site) parseOrderings(input string) {
	s.Orderings = make(map[string]string, 9)
	for _, match := range s.OrderingREGEXP.FindAllStringSubmatch(input, -1) {
		ordering := strings.ToLower(match[2])
		s.Orderings[ordering] = match[1]
	}
	return
}

func (s *Site) FindCategory(group string, category string) (*Category, error) {
	if s.Categories == nil {
		return nil, fmt.Errorf("Categories not loaded")
	}
	if category == "" {
		return nil, fmt.Errorf("Category not specified")
	}
	if group != "" {
		categories, present := s.Categories[group]
		if !present {
			return nil, fmt.Errorf("Category group '%s' not found", group)
		}
		value, present := categories[category]
		if !present {
			return nil, fmt.Errorf("Category '%s/%s' not found", group, category)
		}
		return &Category{
			Group: group,
			Title: category,
			ID:    value,
		}, nil
	}
	var foundCat *Category
	for group, categories := range s.Categories {
		for cat, value := range categories {
			if cat == category {
				if foundCat != nil {
					return nil, fmt.Errorf("Category '%s' is ambiguous, please specify group", category)
				}
				foundCat = &Category{
					Group: group,
					Title: category,
					ID:    value,
				}
			}
		}
	}
	if foundCat == nil {
		return nil, fmt.Errorf("Category '%s' not found", category)
	}
	return foundCat, nil
}

func (s *Site) FindOrdering(ordering string) (*Ordering, error) {
	if s.Orderings == nil {
		return nil, fmt.Errorf("Orderings not loaded")
	}
	if ordering == "" {
		return nil, fmt.Errorf("Ordering not specified")
	}
	value, present := s.Orderings[ordering]
	if !present {
		return nil, fmt.Errorf("Ordering '%s' not found", ordering)
	}
	return &Ordering{
		Title: ordering,
		ID:    value,
	}, nil
}

type Torrent struct {
	Site
	Category
	ID       string
	Title    string
	Magnet   string
	Uploaded time.Time
	User     string
	VIPUser  bool
	Size     int64
	Seeders  int
	Leechers int
	Files    []*File
}

func (t *Torrent) String() string {
	return fmt.Sprintf("%s (%s)", t.Title, t.ID)
}

func (s *Site) parseSearch(input string) []*Torrent {
	var torrents []*Torrent
	var cat Category
	var isVIP bool
	for _, match := range s.SearchREGEXP.FindAllStringSubmatch(input, -1) {
		group := strings.ToLower(match[1])
		catID := match[2]
		category := strings.ToLower(match[3])
		cat = Category{
			Group: group,
			Title: category,
			ID:    catID,
		}
		id := match[4]
		title := match[5]
		magnet := match[6]
		if match[7] == "vip" {
			isVIP = true
		} else {
			isVIP = false
		}
		stamp, err := parseDate(match[8])
		if err != nil {
			s.Logger.Println("Error parsing date from '%s': %s", match[8], err)
		}
		size := parseSize(match[9])
		if size < 0 {
			s.Logger.Println("Error parsing size from '%s'", match[9])
		}
		uploader := match[10]
		seeders, err := strconv.Atoi(match[11])
		if err != nil {
			s.Logger.Println("Error parsing seeders from '%s'", match[11])
			seeders = -1
		}
		leechers, err := strconv.Atoi(match[12])
		if err != nil {
			s.Logger.Println("Error parsing leechers from '%s'", match[12])
			leechers = -1
		}
		torrents = append(torrents, &Torrent{
			Site:     *s,
			Category: cat,
			ID:       id,
			Title:    title,
			Magnet:   magnet,
			Uploaded: stamp,
			User:     uploader,
			VIPUser:  isVIP,
			Size:     size,
			Seeders:  seeders,
			Leechers: leechers,
		})
	}
	return torrents
}

func removeHTML(input string) string {
	output := killHTMLRegexp.ReplaceAllString(input, "")
	return strings.Replace(output, "&nbsp;", " ", -1)
}

func parseSize(input string) int64 {
	input = removeHTML(input)
	multiplier := int64(1)
	parts := strings.Split(input, " ")
	if len(parts) != 2 {
		return -1
	}
	rawSize, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return -1
	}
	switch parts[1] {
	case "TiB":
		multiplier = 1024 * 1024 * 1024 * 1024
	case "GiB":
		multiplier = 1024 * 1024 * 1024
	case "MiB":
		multiplier = 1024 * 1024
	case "KiB":
		multiplier = 1024
	}
	return int64(rawSize * float64(multiplier))
}

func makeOffsetDate(ref time.Time, offset time.Duration, hour, minute int) time.Time {
	ref = ref.Add(offset)
	if hour == -1 || minute == -1 {
		return ref
	}
	return time.Date(
		ref.Year(),
		ref.Month(),
		ref.Day(),
		hour,
		minute,
		0, 0,
		ref.Location(),
	)
}

func parseDate(input string) (time.Time, error) {
	input = removeHTML(input)
	parts := strings.Split(input, " ")
	reference := time.Now()
	if len(parts) < 2 {
		return reference, fmt.Errorf("Not enough string parts")
	}
	if parts[len(parts)-1] == "ago" {
		mins, err := strconv.Atoi(parts[0])
		if err != nil {
			return reference, fmt.Errorf("Cloudn't parse minutes ago")
		}
		return reference.Add(time.Duration(-mins) * time.Minute), nil
	}
	if parts[0] == "Today" {
		parsed, err := time.Parse("15:04", parts[1])
		if err != nil {
			return reference, fmt.Errorf("Couldn't parse today")
		}
		return makeOffsetDate(reference, 0, parsed.Hour(), parsed.Minute()), nil
	}
	if parts[0] == "Y-day" {
		parsed, err := time.Parse("15:04", parts[1])
		if err != nil {
			return reference, fmt.Errorf("Couldn't parse y-day")
		}
		return makeOffsetDate(reference, -24*time.Hour, parsed.Hour(), parsed.Minute()), nil
	}
	parsed, err := time.Parse("01-02 15:04", input)
	if err == nil {
		return time.Date(
			reference.Year(),
			parsed.Month(),
			parsed.Day(),
			parsed.Hour(),
			parsed.Minute(),
			0, 0,
			reference.Location(),
		), nil
	}
	return time.Parse("01-02 2006", input)
}

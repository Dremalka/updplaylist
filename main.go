package main

import (
	"bufio"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/go-ini/ini"
	"log"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type progr struct {
	channel        string
	nameChannel    string
	datepr         time.Time
	timepr         time.Time
	TimeBeginProgr string
	NameProgr      string
	HrefProgr      string
	IDProgr        string
	Day            string
	DayOfWeek      string
	DataProgr      time.Time
}

type listDay struct {
	channel     string
	nameChannel string
	Day         string
	DayOfWeek   string
	URL         string
	DataProgr   time.Time
}

type settings struct {
	updsetdelay  int
	upddatadelay int
	pathplaylist string
	channels     []*ini.Key
	workers      int
}

const (
	NAME_INI_FILE   = "updplaylist.ini"
	DEFUPDSETDELAY  = "600"
	DEFUPDDATADELAY = "3600"
	DEFPATHPLAYLIST = "playlist.m3u"
)

var DEFWORKERS = runtime.NumCPU()

var cf *ini.File
var cfstruct settings
var mutex = &sync.Mutex{}
var chPr map[string][]progr

func main() {

	cfstruct = settings{}

	// считать настройки. при необходимости инициализировать значениями по умолчанию
	var err error
	err = reloadSettings()
	if err != nil {
		if os.IsNotExist(err) {
			cf = ini.Empty()
			err = setDefaultSettings()
			if err != nil {
				panic(err)
			}
			err = cf.SaveTo(NAME_INI_FILE)
			if err != nil {
				panic(err)
			}
		} else {
			panic(err)
		}
	}

	go updSettings()
	go updProgr()

	var response string
	fmt.Println("Press Enter")
	_, _ = fmt.Scanln(&response)
	fmt.Println("Exit.")

}

func reloadSettings() error {
	var value *ini.Key
	var err error

	cf, err = ini.Load(NAME_INI_FILE)
	if err != nil {
		return err
	}

	section, err := cf.GetSection("general")
	if err != nil {
		section, err = cf.NewSection("general")
		if err != nil {
			return err
		}
		section.Comment = "Основные настройки"
	}

	// перечитывать настройки каждые .... сек
	value, err = section.GetKey("updsetdelay")
	if err != nil {
		key, err := section.NewKey("updsetdelay", DEFUPDSETDELAY)
		if err != nil {
			return err
		}
		key.Comment = "Перечитывать настройки каждые ... сек."
		cfstruct.updsetdelay, _ = strconv.Atoi(DEFUPDSETDELAY)
	}
	cfstruct.updsetdelay, err = value.Int()
	if err != nil {
		return err
	}

	// обновлять данные плейлиста каждые .... сек
	value, err = section.GetKey("upddatadelay")
	if err != nil {
		key, err := section.NewKey("upddatadelay", DEFUPDDATADELAY)
		if err != nil {
			return err
		}
		key.Comment = "Обновлять данные плейлиста каждые ... сек."
		cfstruct.upddatadelay, _ = strconv.Atoi(DEFUPDDATADELAY)
	}
	cfstruct.upddatadelay, err = value.Int()
	if err != nil {
		return err
	}

	// Имя файла плейлиста и путь до него.
	value, err = section.GetKey("pathplaylist")
	if err != nil {
		key, err := section.NewKey("pathplaylist", DEFPATHPLAYLIST)
		if err != nil {
			return err
		}
		key.Comment = "Имя файла плейлиста и путь до него."
		cfstruct.pathplaylist = DEFPATHPLAYLIST
	}
	cfstruct.pathplaylist = value.String()
	if err != nil {
		return err
	}

	// Количество параллельных потоков для парсинга сайта
	value, err = section.GetKey("workers")
	if err != nil {
		key, err := section.NewKey("workers", strconv.Itoa(DEFWORKERS))
		if err != nil {
			return err
		}
		key.Comment = "Количество параллельных потоков для парсинга сайта. По умолчанию равен кол-ву ядер процессора."
		cfstruct.workers = DEFWORKERS
	}
	cfstruct.workers, err = value.Int()
	if err != nil {
		return err
	}

	// каналы
	section, err = cf.GetSection("channels")
	if err != nil {
		section, err = cf.NewSection("channels")
		if err != nil {
			return err
		}
	}
	ch := section.Keys()
	cfstruct.channels = ch

	return nil
}

func setDefaultSettings() error {
	section, err := cf.NewSection("general")
	if err != nil {
		return err
	}
	section.Comment = "Основные настройки"

	key, err := section.NewKey("updsetdelay", DEFUPDSETDELAY)
	if err != nil {
		return err
	}
	key.Comment = "Перечитывать настройки каждые ... сек."

	key, err = section.NewKey("upddatadelay", DEFUPDDATADELAY)
	if err != nil {
		return err
	}
	key.Comment = "Обновлять данные плейлиста каждые ... сек."

	key, err = section.NewKey("pathplaylist", DEFPATHPLAYLIST)
	if err != nil {
		return err
	}
	key.Comment = "Имя файла плейлиста и путь до него."

	key, err = section.NewKey("workers", strconv.Itoa(DEFWORKERS))
	if err != nil {
		return err
	}
	key.Comment = "Количество параллельных потоков для парсинга сайта. По умолчанию равен кол-ву ядер процессора."

	section, err = cf.NewSection("channels")
	if err != nil {
		return err
	}
	section.Comment = "Список каналов. Пример строки: -:rossija"

	return nil
}

func updSettings() {
	for {
		mutex.Lock()
		err := reloadSettings()
		mutex.Unlock()
		if err != nil {
			fmt.Println(err)
		}
		fmt.Println("Обновление настроек")
		time.Sleep(time.Duration(cfstruct.updsetdelay) * time.Second)
	}
}

func updProgr() {
	for {
		fmt.Println("Обновление каналов")
		mutex.Lock()

		channelInCollectDataProgr := make(chan progr, 50)
		channelDoneCollectDataProgr := make(chan struct{})
		done := make(chan struct{})
		go collectDataProgr(channelInCollectDataProgr, channelDoneCollectDataProgr, done)

		listURL := getListURL(cfstruct.channels)

		chURL := make(chan listDay)
		for i := 0; i < cfstruct.workers; i++ {
			go getProgr(chURL, channelInCollectDataProgr, channelDoneCollectDataProgr)
		}

		for _, rec := range listURL {
			chURL <- rec
		}
		close(chURL)
		<-done

		close(channelInCollectDataProgr)
		close(channelDoneCollectDataProgr)
		close(done)

		// сортировать массив с данными
		//		channel := func(c1, c2 *progr) bool {
		//			return c1.channel < c2.channel
		//		}
		DataProg := func(c1, c2 *progr) bool {
			return c1.DataProgr.After(c2.DataProgr)
		}

		datepr := func(c1, c2 *progr) bool {
			return c1.datepr.Before(c2.datepr)
		}

		timepr := func(c1, c2 *progr) bool {
			return c1.timepr.Before(c2.timepr)
		}

		for key, vol := range chPr {
			OrderBy(DataProg, datepr, timepr).Sort(vol)
			chPr[key] = vol
		}

		//		for _, vol := range chPr {
		//			for _, vol1 := range vol {
		//				fmt.Println(vol1.channel, vol1.date, vol1.time, vol1.TimeBeginProgr, vol1.NameProgr, vol1.IDProgr, vol1.HrefProgr)
		//			}
		//		}

		// обновить файл playlist
		linesText, err := readLines(cfstruct.pathplaylist)
		if err != nil {
			fmt.Println(err, linesText)
			return
		}
		linesText, err = checkLines(linesText)

		//fmt.Println(linesText)

		//		for key, vol := range chPr {
		//			fmt.Println(key)
		//			for _, vol := range vol {
		//				fmt.Println(vol)
		//			}
		//		}

		newlinesText := make([]string, 0)
		for _, str := range linesText {
			newlinesText = append(newlinesText, str)
			if strings.HasPrefix(str, "#archive-begin") {
				strSplit := strings.Split(str, "-")
				ch := strSplit[len(strSplit)-1]
				flag := true
				for _, vol := range chPr[ch] {
					var serviceInf string
					if flag {
						serviceInf = `aspect-ratio=4:3 group-title="` + vol.nameChannel + ` (архив)",`
						flag = false
					} else {
						serviceInf = "aspect-ratio=4:3,"
					}
					firststr := "#EXTINF:-1 " + serviceInf + vol.Day + " " + vol.DayOfWeek + " " + vol.TimeBeginProgr + ` "` + vol.NameProgr + `"`
					newlinesText = append(newlinesText, firststr)

					secondstr := "http://hls.peers.tv/playlist/program/" + vol.IDProgr + ".m3u8"
					newlinesText = append(newlinesText, secondstr)
				}
			}
		}
		linesText = newlinesText

		//		for _, str := range linesText {
		//			fmt.Println(str)
		//		}

		//	for ind := 0; ind < len(linesText); ind++ {
		//		if linesText[ind] == ("#archive-"+chname+"-begin") && linesText[ind+1] != ("#archive-"+chname+"-end") {
		//			splString := strings.SplitAfter(linesText[ind+1], "aspect-ratio=4:3")
		//			linesText[ind+1] = splString[0] + ` group-title="Россия 1 (архив)"` + splString[1]
		//		}
		//	}
		//
		//	//	for _, line := range linesText {
		//	//		fmt.Println(line)
		//	//	}
		//
		if err := writeLines(linesText, cfstruct.pathplaylist); err != nil {
			log.Fatalf("writeLines: %s", err)
		}

		//
		//		var thisChName string
		//		for _, vol := range listProgr {
		//			if vol.channel != thisChName {
		//				var indBeginChannek, indEndChannel int
		//				for _, vol := range linesText {
		//					switch vol {
		//					case :
		//
		//					}
		//				}
		//			}
		//		}
		//
		//
		//			var newLinesText []string
		//			strFound := false
		//			for _, line := range linesText {
		//				if strFound == false || line == ("#archive-"+chname+"-end") {
		//					newLinesText = append(newLinesText, line)
		//				}
		//				if line == ("#archive-" + chname + "-begin") {
		//					strFound = true
		//				}
		//				if line == ("#archive-" + chname + "-end") {
		//					strFound = false
		//				}
		//			}
		//			linesText = newLinesText
		//
		//			for _, thisDay := range listDays {
		//
		//				var newLinesText []string
		//				for _, line := range linesText {
		//					newLinesText = append(newLinesText, line)
		//					//fmt.Println(line, string("#archive-"+chname+"-begin"))
		//					if line == string("#archive-"+chname+"-begin") {
		//						//fmt.Println("11111", line)
		//						for _, strProgr := range listProgr {
		//							firststr := "#EXTINF:-1 aspect-ratio=4:3," + `"` + thisDay.Day + " " + thisDay.DayOfWeek + " " + strProgr.TimeBeginProgr + " " + strProgr.NameProgr + `"`
		//							newLinesText = append(newLinesText, firststr)
		//
		//							secondstr := "http://hls.peers.tv/playlist/program/" + strProgr.IDProgr + ".m3u8"
		//							newLinesText = append(newLinesText, secondstr)
		//						}
		//					}
		//
		//				}
		//				linesText = newLinesText
		//			}
		//

		fmt.Println("Выполнено!")

		//		for _, chkey := range cfstruct.channels {
		//			namechannel := chkey.Value()
		//			err := getProgr(namechannel)
		//			if err != nil {
		//				fmt.Println(err)
		//			}
		//		}
		//
		//		channelDoneCollectDataProgr <- struct{}
		mutex.Unlock()

		time.Sleep(time.Duration(cfstruct.upddatadelay) * time.Second)
	}
}

func collectDataProgr(in <-chan progr, done <-chan struct{}, genDone chan<- struct{}) {
	chPr = make(map[string][]progr)
	workers := cfstruct.workers
loop:
	for {
		select {
		case recpr := <-in:
			chPr[recpr.channel] = append(chPr[recpr.channel], recpr)

		case <-done:
			workers--
			if workers <= 0 {
				//				for _, vol := range listProgr {
				//					fmt.Println(vol.channel, vol.date, vol.time, vol.TimeBeginProgr, vol.NameProgr, vol.IDProgr, vol.HrefProgr)
				//				}
				break loop
			}
		}
	}
	genDone <- struct{}{}
	return
}

func getProgr(in <-chan listDay, out chan<- progr, done chan<- struct{}) {

	for thisDay := range in {
		listProgr, err := getListProgr(thisDay.URL)
		if err != nil {
			fmt.Println(err)
		}
		for _, vol := range listProgr {
			progr := progr{}
			progr.channel = thisDay.channel
			progr.nameChannel = thisDay.nameChannel
			progr.datepr = vol.datepr
			progr.timepr = vol.timepr
			progr.TimeBeginProgr = vol.TimeBeginProgr
			progr.IDProgr = vol.IDProgr
			progr.NameProgr = vol.NameProgr
			progr.HrefProgr = vol.HrefProgr
			progr.Day = thisDay.Day
			progr.DayOfWeek = thisDay.DayOfWeek
			progr.DataProgr = thisDay.DataProgr
			out <- progr
		}

	}
	done <- struct{}{}

	//	linesText, err := readLines(cfstruct.pathplaylist)
	//	if err != nil {
	//		fmt.Println(err)
	//		return err
	//	}
	//
	//	var newLinesText []string
	//	strFound := false
	//	for _, line := range linesText {
	//		if strFound == false || line == ("#archive-"+chname+"-end") {
	//			newLinesText = append(newLinesText, line)
	//		}
	//		if line == ("#archive-" + chname + "-begin") {
	//			strFound = true
	//		}
	//		if line == ("#archive-" + chname + "-end") {
	//			strFound = false
	//		}
	//	}
	//	linesText = newLinesText
	//
	//	for _, thisDay := range listDays {

	//	var newLinesText []string
	//	for _, line := range linesText {
	//		newLinesText = append(newLinesText, line)
	//		//fmt.Println(line, string("#archive-"+chname+"-begin"))
	//		if line == string("#archive-"+chname+"-begin") {
	//			//fmt.Println("11111", line)
	//			for _, strProgr := range listProgr {
	//				firststr := "#EXTINF:-1 aspect-ratio=4:3," + `"` + thisDay.Day + " " + thisDay.DayOfWeek + " " + strProgr.TimeBeginProgr + " " + strProgr.NameProgr + `"`
	//				newLinesText = append(newLinesText, firststr)
	//
	//				secondstr := "http://hls.peers.tv/playlist/program/" + strProgr.IDProgr + ".m3u8"
	//				newLinesText = append(newLinesText, secondstr)
	//			}
	//		}
	//
	//	}
	//	linesText = newLinesText
	//

	//fmt.Println(url, listProgr)
	//break
	//	}
	//
	//	for ind := 0; ind < len(linesText); ind++ {
	//		if linesText[ind] == ("#archive-"+chname+"-begin") && linesText[ind+1] != ("#archive-"+chname+"-end") {
	//			splString := strings.SplitAfter(linesText[ind+1], "aspect-ratio=4:3")
	//			linesText[ind+1] = splString[0] + ` group-title="Россия 1 (архив)"` + splString[1]
	//		}
	//	}
	//
	//	//	for _, line := range linesText {
	//	//		fmt.Println(line)
	//	//	}
	//
	//	if err := writeLines(linesText, cfstruct.pathplaylist); err != nil {
	//		log.Fatalf("writeLines: %s", err)
	//	}
	return
}

func getListURL(channelsKeys []*ini.Key) []listDay {
	var list []listDay
	for _, channelKey := range channelsKeys {
		channel := channelKey.Value()
		doc, err := goquery.NewDocument("http://www.cn.ru/tv/program/" + channel + "/")
		if err != nil {
			log.Fatal(err)
		}

		nameChannel := doc.Find("#cn-ru #master.cn-master #cnbody.cnbody #graycontainer #container.no-padding.scnt .tv-inner-content h2.prg-channel span").Text()

		doc.Find("#cn-ru #master.cn-master #cnbody.cnbody #graycontainer #container.no-padding.scnt .tv-inner-content #mtvprg-week.prg-week a").Each(func(i int, s *goquery.Selection) {
			if articleURL, ok := s.Attr("href"); ok {
				thisDay := listDay{}
				thisDay.nameChannel = nameChannel
				articleURLSplit := strings.Split(articleURL, "/")
				thisDay.DataProgr, _ = time.Parse("2006-01-02", articleURLSplit[len(articleURLSplit)-2])
				thisDay.channel = channel
				thisDay.URL = articleURL
				thisDay.Day = s.Find("strong").Text()
				thisDay.DayOfWeek = s.Find("small").Text()
				list = append(list, thisDay)
			}
		})
	}

	return list
}

func getListProgr(url string) ([]progr, error) {
	var listProgr []progr
	sourceURL := "http://www.cn.ru" + url
	fmt.Println(sourceURL)

	doc, err := goquery.NewDocument(sourceURL)
	if err != nil {
		log.Fatal(err)
	}
	doc.Find("#cn-ru #master.cn-master #cnbody.cnbody #graycontainer #container.no-padding.scnt .tv-inner-content #mtvprg-program.prg-list ol li").Each(func(i int, s *goquery.Selection) {
		//fmt.Println(s.Html())
		s.Find(".tlcbar.is-able").Each(func(i int, s *goquery.Selection) {
			strProgr := progr{}
			timeBeginProgr := s.Find("ins").Text()
			nameProgr := s.Find("dfn a").Text()

			hrefDate, _ := s.Find("ins a").Attr("href")
			splitStr := strings.Split(hrefDate, "/")
			strDate := splitStr[len(splitStr)-2]
			dateTime, _ := time.Parse("2006-01-02T15:04:05-0700", strDate)
			yearPr, monthPr, dayPr := dateTime.Date()
			datePr := time.Date(yearPr, monthPr, dayPr, 0, 0, 0, 0, dateTime.Location())

			hrefProgr, _ := s.Find("dfn a").Attr("href")
			splitStr = strings.Split(hrefProgr, "/")
			id := splitStr[len(splitStr)-2]

			//fmt.Println(s.Html())
			//fmt.Println(timeBeginProgr)
			strProgr.datepr = datePr
			strProgr.timepr = dateTime
			strProgr.TimeBeginProgr = timeBeginProgr
			strProgr.NameProgr = nameProgr
			strProgr.HrefProgr = hrefProgr
			strProgr.IDProgr = id
			listProgr = append(listProgr, strProgr)
		})
	})

	return listProgr, nil
}

func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {

		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func checkLines(lines []string) ([]string, error) {
	var foundBegin bool
	var newlines []string
	var listch []string
	for _, key := range cfstruct.channels {
		listch = append(listch, key.Value())
	}

	for _, str := range lines {
		if strings.HasPrefix(str, "#archive-end") {
			foundBegin = false
		}
		if strings.HasPrefix(str, "#archive-begin") && !foundBegin {
			strSplit := strings.Split(str, "-")
			channel := strSplit[len(strSplit)-1]
		loop:
			for i, vol := range listch {
				if vol == channel {
					newlistch := make([]string, 0)
					newlistch = append(newlistch, listch[:i]...)
					newlistch = append(newlistch, listch[i+1:]...)
					listch = newlistch
					break loop
				}
			}
			newlines = append(newlines, str)
			foundBegin = true
		} else if !foundBegin {
			newlines = append(newlines, str)
		}
	}
	for _, vol := range listch {
		newlines = append(newlines, "#archive-begin-"+vol)
		newlines = append(newlines, "#archive-end")
	}
	return newlines, nil
}

func writeLines(lines []string, path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	w := bufio.NewWriter(file)
	for _, line := range lines {
		fmt.Fprintln(w, line)
	}
	return w.Flush()
}

type lessFunc func(p1, p2 *progr) bool

type multiSorter struct {
	bs   []progr
	less []lessFunc
}

func (ms *multiSorter) Sort(bs []progr) {
	ms.bs = bs
	sort.Sort(ms)
}

func OrderBy(less ...lessFunc) *multiSorter {
	return &multiSorter{
		less: less,
	}
}

func (ms *multiSorter) Len() int {
	return len(ms.bs)
}

func (ms *multiSorter) Swap(i, j int) {
	ms.bs[i], ms.bs[j] = ms.bs[j], ms.bs[i]
}

func (ms *multiSorter) Less(i, j int) bool {
	p, q := &ms.bs[i], &ms.bs[j]
	// Try all but the last comparison.
	var k int
	for k = 0; k < len(ms.less)-1; k++ {
		less := ms.less[k]
		switch {
		case less(p, q):
			// p < q, so we have a decision.
			return true
		case less(q, p):
			// p > q, so we have a decision.
			return false
		}
		// p == q; try the next comparison.
	}
	// All comparisons to here said "equal", so just return whatever
	// the final comparison reports.
	return ms.less[k](p, q)
}

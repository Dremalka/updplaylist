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

// структура записи программы передач
type progr struct {
	channel        string
	nameChannel    string
	datepr         time.Time
	timepr         time.Time
	timeBeginProgr string
	nameProgr      string
	hrefProgr      string
	idProgr        string
	day            string
	dayOfWeek      string
	dataProgr      time.Time
}

// структура записи канала
type listDay struct {
	channel     string
	nameChannel string
	day         string
	dayOfWeek   string
	url         string
	dataProgr   time.Time
}

// настройки
type settings struct {
	updsetdelay  int
	upddatadelay int
	pathplaylist string
	channels     []*ini.Key
	workers      int
}

const (
	nameIniFile     = "updplaylist.ini" // имя файла с настройками
	defUpdSetDelay  = "600"             // периодичность с которой перечитывать файл с настройками
	defUpdDataDelay = "3600"            // периодичность с которой обновлять плейлист
	defPathPlaylist = "playlist.m3u"    // имя файла-плейлиста
)

var defWorkers = runtime.NumCPU() // количество параллельных потоков при загрузке данных с сайта www.cn.ru

var cf *ini.File      // объект пакета ini с данными настройки
var cfstruct settings // настройки программы
var mutex = &sync.Mutex{}
var chPr map[string][]progr // отображение массивов с данными программы передач

func main() {

	cfstruct = settings{}

	// считать настройки. при необходимости инициализировать значениями по умолчанию
	var err error
	err = reloadSettings()
	if err != nil {
		log.Panicln("Ошибка при загрузке файла с настройками:", err)
	}

	go updSettings() // горутина периодически перечитывает настройки
	go updProgr()    // горутина периодически собирает данные с сайта и обновляет плейлист

	// для выхода из программы ждать нажатия кнопки
	var response string
	fmt.Println("Press Enter")
	_, _ = fmt.Scanln(&response)
	fmt.Println("Exit.")

}

// reload считывает данные с ini-файла и загружает в структуру. При необходимости инициализирует данные значениями по умолчанию
func reloadSettings() error {
	var key *ini.Key
	var err error
	var value int

	defUpdSetDelayInt, _ := strconv.Atoi(defUpdSetDelay)
	defUpdDataDelayInt, _ := strconv.Atoi(defUpdDataDelay)

	// открыть ini-файл
	cf, err = ini.Load(nameIniFile)
	if err != nil {
		if os.IsNotExist(err) { // файл с настройками не найден?
			cf = ini.Empty() // создать новый объект с настройками
		} else {
			return err
		}
	}

	// обработать секцию "general" с основными настройками
	section, err := cf.GetSection("general")
	if err != nil {
		section, err = cf.NewSection("general") // секции нет в ini-файле? Тогда создать.
		if err != nil {
			return err // если не удалось создать, то продолжать бессмысленно
		}
		section.Comment = "Основные настройки"
	}

	// перечитывать настройки каждые .... сек
	key, err = section.GetKey("updsetdelay")
	if err != nil {
		key, err = section.NewKey("updsetdelay", defUpdSetDelay)
		if err != nil {
			return err
		}
		key.Comment = "Перечитывать настройки каждые ... сек."
	}
	value = key.RangeInt(defUpdSetDelayInt, 5, 1000000) // значение в пределах 5 - 1000000 секунд. При ошибке инициализация значением по умолчанию
	key.SetValue(strconv.Itoa(value))
	cfstruct.updsetdelay = value

	// обновлять данные плейлиста каждые .... сек
	key, err = section.GetKey("upddatadelay")
	if err != nil {
		key, err = section.NewKey("upddatadelay", defUpdDataDelay)
		if err != nil {
			return err
		}
		key.Comment = "Обновлять данные плейлиста каждые ... сек."
	}
	value = key.RangeInt(defUpdDataDelayInt, 300, 1000000) // значение в пределах 300 - 1000000 секунд. При ошибке инициализация значением по умолчанию
	key.SetValue(strconv.Itoa(value))
	cfstruct.upddatadelay = value

	// Имя файла плейлиста и путь до него.
	key, err = section.GetKey("pathplaylist")
	if err != nil {
		key, err = section.NewKey("pathplaylist", defPathPlaylist)
		if err != nil {
			return err
		}
		key.Comment = "Имя файла плейлиста и путь до него."
	}
	cfstruct.pathplaylist = key.String()

	// Количество параллельных потоков для парсинга сайта
	key, err = section.GetKey("workers")
	if err != nil {
		key, err = section.NewKey("workers", strconv.Itoa(defWorkers))
		if err != nil {
			return err
		}
		key.Comment = "Количество параллельных потоков для парсинга сайта. По умолчанию равен кол-ву ядер процессора."

	}
	value = key.RangeInt(defWorkers, 1, 100) // значение в пределах 1 - 100 отдельных потоков. При ошибке инициализация значением по умолчанию
	key.SetValue(strconv.Itoa(value))
	cfstruct.workers = value

	// секция "каналы"
	section, err = cf.GetSection("channels")
	if err != nil {
		section, err = cf.NewSection("channels") // секции нет в ini-файле? Создать секцию.
		if err != nil {
			return err
		}
	}
	section.Comment = "Список каналов. Пример строки: -:rossija"

	// секция содержит список каналов
	ch := section.Keys() // получить массив списка каналов
	cfstruct.channels = ch

	err = cf.SaveTo(nameIniFile) // сохранить файл с значениями по умолчанию
	if err != nil {
		return err
	}

	return nil
}

// updSettings с заданной перидочностью из ini-файла обновляет настройки
func updSettings() {
	for {
		mutex.Lock()
		err := reloadSettings()
		if err != nil {
			log.Panicln("Ошибка при загрузке файла с настройками:", err)
		}
		mutex.Unlock()
		log.Println("Настройки обновлены")
		time.Sleep(time.Duration(cfstruct.updsetdelay) * time.Second)
	}
}

// updProgr с заданной периодичностью обновляет плейлист
func updProgr() {
	for {
		log.Println("Обновляется плейлист")
		mutex.Lock()

		channelInCollectDataProgr := make(chan progr, 200) // канал по которому пул горутин передает сборщику записи с данными по каждой программе передач
		channelDoneCollectDataProgr := make(chan struct{}) // канал по которому каждая горутина сообщают сборщику о прекращении обработки данных и закрытии
		done := make(chan struct{})                        // канал по которому сборщик данных сообщает текущей функции о том, что все данные собраны

		go collectDataProgr(channelInCollectDataProgr, channelDoneCollectDataProgr, done) // запустить сборщик данных

		listURL := getListURL(cfstruct.channels) // получить массив с данными (включая ссылку на страницу) для каждого дня заданных каналов

		chURL := make(chan listDay)             // канал по которому пулу горутин передается структура с данными (включая ссылку на страницу) каждого дня канала
		for i := 0; i < cfstruct.workers; i++ { // создать пул горутин
			go getProgr(chURL, channelInCollectDataProgr, channelDoneCollectDataProgr)
		}

		for _, rec := range listURL {
			chURL <- rec // передать горутинам все ссылки (для каждого канала, каждый день)
		}
		close(chURL) // за ненадобностью закрыть канал
		<-done       // и ждать завершения работы сборщика

		close(channelInCollectDataProgr) // закрыть все созданные каналы
		close(channelDoneCollectDataProgr)
		close(done)

		// настроить условия сортировки массива с основными данными и рассортировать подготовленные данные
		dataProg := func(c1, c2 *progr) bool { // дни программы передач сортировать по убыванию (... 5, 4, 3, 2,..,)
			return c1.dataProgr.After(c2.dataProgr)
		}

		datepr := func(c1, c2 *progr) bool { // дни внутри одного дня программы передач сортировать по возрастанию. Бывает, что в программе передач передачи заканчиваются ночью следующего дня
			return c1.datepr.Before(c2.datepr)
		}

		timepr := func(c1, c2 *progr) bool { // время внутри одного дня программы передач сортировать возрастанию.
			return c1.timepr.Before(c2.timepr)
		}

		for key, vol := range chPr { // каждый массив программ передач канала
			orderBy(dataProg, datepr, timepr).Sort(vol) // рассортировать понастроенным выше правилам
			chPr[key] = vol
		}

		// обработка плейлиста
		linesText, err := readLines(cfstruct.pathplaylist) // прочитать плейлист
		if err != nil {
			log.Println("Ошибка при открытии и считывании плейлиста:", err)
		} else {
			linesText, err = checkLines(linesText) // удалить старые данные между строками-якорями. Создать новые строки-якори для новых каналов (#archive-begin-rossija, #archive-end,...)
			if err != nil {
				log.Println("Ошибка при подготовке плейлиста к обновлению:", err)
			} else {
				// обойти все строки плейлиста. При получении строки-якоря заполнить новыми данными
				var newlinesText []string
			loop:
				for _, str := range linesText {
					newlinesText = append(newlinesText, str)      // обычные строки плейлиста. Не обрабатываются.
					if strings.HasPrefix(str, "#archive-begin") { // строка-якорь начала данных определенного канала
						strSplit := strings.Split(str, "-")
						if len(strSplit) != 3 {
							log.Printf("Ошибка в строке: %s. Правильный пример: #archive-begin-rossija\n", str)
							continue loop
						}
						ch := strSplit[2] // получить название канала
						flag := true
						for _, vol := range chPr[ch] { // найти в отображении массив данных заданного канала
							var serviceInf string
							if flag { // в первой строке нужно задать имя группы
								serviceInf = `aspect-ratio=4:3 group-title="` + vol.nameChannel + ` (архив)",`
								flag = false
							} else {
								serviceInf = "aspect-ratio=4:3,"
							}

							// сформировать две строки в формате m3u
							firststr := "#EXTINF:-1 " + serviceInf + vol.day + " " + vol.dayOfWeek + " " + vol.timeBeginProgr + ` "` + vol.nameProgr + `"`
							newlinesText = append(newlinesText, firststr)

							secondstr := "http://hls.peers.tv/playlist/program/" + vol.idProgr + ".m3u8"
							newlinesText = append(newlinesText, secondstr)
						}
					}
				}
				linesText = newlinesText

				// записать обновленный плейлист в файл
				err := writeLines(linesText, cfstruct.pathplaylist)
				if err != nil {
					log.Printf("Ошибка при записи новых данных в файл %s.\n", cfstruct.pathplaylist)
				}

			}
		}
		mutex.Unlock()
		log.Println("Обновление плейлиста завершено")

		time.Sleep(time.Duration(cfstruct.upddatadelay) * time.Second)
	}
}

// collectDataProg сборщик собирает из канала записи и складывает в массив
func collectDataProgr(in <-chan progr, done <-chan struct{}, genDone chan<- struct{}) {
	chPr = make(map[string][]progr) // отображение. В качестве ключа - название канала. Значение - массив с данными по каналу
	workers := cfstruct.workers     // количество горутин. По количеству определяется момент, когда необходимо завершить работу.
loop:
	for {
		select {
		case recpr := <-in: // полученную запись из канала
			chPr[recpr.channel] = append(chPr[recpr.channel], recpr) // сохранить в массив

		case <-done: // горутина вернула сигнал о завершении работы
			workers--
			if workers <= 0 { // как только все горутины вернут сигнал о завершении
				break loop // прервать цикл
			}
		}
	}
	genDone <- struct{}{} // отправить сигнал о завершении вызываемой функции
	return
}

// getProg по каждому дню получает массив данных программы передач. Собранные данные отправляет по каналу сборщику. URL страницы получает из канала
func getProgr(in <-chan listDay, out chan<- progr, done chan<- struct{}) {

loop:
	for thisDay := range in { // получить очередной URL страницы
		listProgr, err := getListProgr(thisDay.url) // URL передать функции. Обратно получить массив с данными.
		if err != nil {
			log.Printf("Ошибка при получении данных программы передач. Канал = %s, URL=%s\n", thisDay.channel, thisDay.url)
			continue loop
		}
		for _, vol := range listProgr { // каждую запись программы сформировать отдельно
			progr := progr{}
			progr.channel = thisDay.channel
			progr.nameChannel = thisDay.nameChannel
			progr.datepr = vol.datepr
			progr.timepr = vol.timepr
			progr.timeBeginProgr = vol.timeBeginProgr
			progr.idProgr = vol.idProgr
			progr.nameProgr = vol.nameProgr
			progr.hrefProgr = vol.hrefProgr
			progr.day = thisDay.day
			progr.dayOfWeek = thisDay.dayOfWeek
			progr.dataProgr = thisDay.dataProgr
			out <- progr // и отправить сборщику
		}

	}
	done <- struct{}{}

	return
}

// getListUrl парсит основную страницу канала. Получает ссылки на каждый день программы передач.
func getListURL(channelsKeys []*ini.Key) []listDay {
	var list []listDay
loop:
	for _, channelKey := range channelsKeys {
		channel := channelKey.Value()
		doc, err := goquery.NewDocument("http://www.cn.ru/tv/program/" + channel + "/")
		if err != nil {
			log.Printf("Ошибка при получении ссылок на каждый день программы передач. Канал = %s.\n", channel)
			continue loop
		}

		nameChannel := doc.Find("#cn-ru #master.cn-master #cnbody.cnbody #graycontainer #container.no-padding.scnt .tv-inner-content h2.prg-channel span").Text()

		doc.Find("#cn-ru #master.cn-master #cnbody.cnbody #graycontainer #container.no-padding.scnt .tv-inner-content #mtvprg-week.prg-week a").Each(func(i int, s *goquery.Selection) {
			if articleURL, ok := s.Attr("href"); ok {
				thisDay := listDay{}
				thisDay.nameChannel = nameChannel
				articleURLSplit := strings.Split(articleURL, "/")
				thisDay.dataProgr, _ = time.Parse("2006-01-02", articleURLSplit[len(articleURLSplit)-2])
				thisDay.channel = channel
				thisDay.url = articleURL
				thisDay.day = s.Find("strong").Text()
				thisDay.dayOfWeek = s.Find("small").Text()
				list = append(list, thisDay)
			}
		})
	}

	return list
}

// getListProgr запрашивает html-страницу. Парсит и собирает данные по программам в массив
func getListProgr(url string) ([]progr, error) {
	var listProgr []progr
	sourceURL := "http://www.cn.ru" + url
	doc, err := goquery.NewDocument(sourceURL)
	if err != nil {
		log.Printf("Ошибка при получении html-страницы. URL = %s\n", url)
		return nil, err
	}
	doc.Find("#cn-ru #master.cn-master #cnbody.cnbody #graycontainer #container.no-padding.scnt .tv-inner-content #mtvprg-program.prg-list ol li").Each(func(i int, s *goquery.Selection) {
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
			strProgr.timeBeginProgr = timeBeginProgr
			strProgr.nameProgr = nameProgr
			strProgr.hrefProgr = hrefProgr
			strProgr.idProgr = id
			listProgr = append(listProgr, strProgr)
		})
	})

	return listProgr, nil
}

// readLines считывает из текстового файла в строковый массив
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

// checkLines удаляет из массив старые данные. Расставляет якорные строки.
func checkLines(lines []string) ([]string, error) {
	var foundBegin bool
	var newlines []string
	var listch []string

	for _, key := range cfstruct.channels {
		listch = append(listch, key.Value())
	}

loop_1:
	for _, str := range lines {
		if strings.HasPrefix(str, "#archive-end") {
			foundBegin = false
		}
		if strings.HasPrefix(str, "#archive-begin") && !foundBegin {
			strSplit := strings.Split(str, "-")
			if len(strSplit) != 3 {
				log.Printf("Ошибка в строке: %s. Правильный пример: #archive-begin-rossija\n", str)
				continue loop_1
			}
			channel := strSplit[2]
		loop:
			for i, vol := range listch {
				if vol == channel {
					var newlistch []string
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

	// если в плейлисте нет строк-якорей для каналов, то создать их в конце файла
	for _, vol := range listch {
		newlines = append(newlines, "#archive-begin-"+vol)
		newlines = append(newlines, "#archive-end")
	}
	return newlines, nil
}

// writeLines записывает обработанный плейлист в файл
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

// сортировка массива структур по полям структуры
type lessFunc func(p1, p2 *progr) bool
type multiSorter struct {
	bs   []progr
	less []lessFunc
}

func (ms *multiSorter) Sort(bs []progr) {
	ms.bs = bs
	sort.Sort(ms)
}

func orderBy(less ...lessFunc) *multiSorter {
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

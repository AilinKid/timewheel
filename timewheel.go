package timewheel

import (
	"container/list"
	"errors"
	"sync"
	"time"
)

// time wheel struct
type TimeWheel struct {
	interval       time.Duration
	ticker         *time.Ticker
	slots          []*list.List
	currentPos     int
	slotNum        int
	addTaskChannel chan *task
	stopChannel    chan bool
	taskRecord     map[interface{}]*task
	recordLock     sync.RWMutex
}

// Job callback function
type Job func(TaskData)

// TaskData callback params
type TaskData map[interface{}]interface{}

// task struct
type task struct {
	interval time.Duration
	times    int //-1:no limit >=1:run times
	circle   int
	key      interface{}
	job      Job
	taskData TaskData
}

// New create a empty time wheel
func New(interval time.Duration, slotNum int) *TimeWheel {
	if interval <= 0 || slotNum <= 0 {
		return nil
	}
	tw := &TimeWheel{
		interval:       interval,
		slots:          make([]*list.List, slotNum),
		currentPos:     0,
		slotNum:        slotNum,
		addTaskChannel: make(chan *task),
		stopChannel:    make(chan bool),
		taskRecord:     make(map[interface{}]*task),
	}

	tw.init()

	return tw
}

// Start start the time wheel
func (tw *TimeWheel) Start() {
	tw.ticker = time.NewTicker(tw.interval)
	go tw.start()
}

// Stop stop the time wheel
func (tw *TimeWheel) Stop() {
	tw.stopChannel <- true
}

func (tw *TimeWheel) start() {
	for {
		select {
		case <-tw.ticker.C:
			tw.tickHandler()
		case task := <- tw.addTaskChannel:
			tw.addTask(task)
		case <-tw.stopChannel:
			tw.ticker.Stop()
			return
		}
	}
}

// AddTask add new task to the time wheel
func (tw *TimeWheel) AddTask(interval time.Duration, times int, key interface{}, data TaskData, job Job) error {
	if interval <= 0 || key == nil || job == nil || times < -1 || times == 0 {
		return errors.New("illegal task params")
	}

	tw.recordLock.RLock()
	_, ok := tw.taskRecord[key]
	tw.recordLock.RUnlock()
	if ok {
		return errors.New("duplicate task key")
	}

	tw.addTaskChannel <- &task{interval: interval, times: times, key: key, taskData: data, job: job}
	return nil
}

// RemoveTask remove the task from time wheel
func (tw *TimeWheel) RemoveTask(key interface{}) error {
	if key == nil {
		return nil
	}

	tw.recordLock.RLock()
	defer tw.recordLock.RUnlock()
	task := tw.taskRecord[key]

	if task == nil {
		return errors.New("task not exists, please check you task key")
	} else {
		// lazy remove task
		task.times = 0
		delete(tw.taskRecord, task.key)
	}
	return nil
}

// UpdateTask update task times and data
func (tw *TimeWheel) UpdateTask(key interface{}, interval time.Duration, taskData TaskData) error {
	if key == nil {
		return errors.New("illegal key, please try again")
	}

	tw.recordLock.RLock()
	task, ok := tw.taskRecord[key]
	tw.recordLock.RUnlock()

	if !ok {
		return errors.New("task not exists, please check you task key")
	}

	task.taskData = taskData
	task.interval = interval
	return nil
}

// time wheel initialize
func (tw *TimeWheel) init() {
	for i := 0; i < tw.slotNum; i++ {
		tw.slots[i] = list.New()
	}
}

//
func (tw *TimeWheel) tickHandler() {
	l := tw.slots[tw.currentPos]
	tw.scanAddRunTask(l)
	if tw.currentPos == tw.slotNum-1 {
		tw.currentPos = 0
	} else {
		tw.currentPos++
	}
}

// add task
func (tw *TimeWheel) addTask(task *task) {
	if task.times == 0 {
		return
	}

	pos, circle := tw.getPositionAndCircle(task.interval)
	task.circle = circle

	tw.slots[pos].PushBack(task)

	//record the task
	tw.recordLock.Lock()
	defer tw.recordLock.Unlock()
	tw.taskRecord[task.key] = task
}

// scan task list and run the task
func (tw *TimeWheel) scanAddRunTask(l *list.List) {

	if l == nil {
		return
	}

	for item := l.Front(); item != nil; {
		task := item.Value.(*task)

		if task.times == 0 {
			next := item.Next()
			l.Remove(item)
			tw.recordLock.Lock()
			delete(tw.taskRecord, task.key)
			tw.recordLock.Unlock()
			item = next
			continue
		}

		if task.circle > 0 {
			task.circle--
			item = item.Next()
			continue
		}

		go task.job(task.taskData)
		next := item.Next()
		l.Remove(item)
		item = next

		if task.times == 1 {
			task.times = 0
			tw.recordLock.Lock()
			delete(tw.taskRecord, task.key)
			tw.recordLock.Unlock()
		} else {
			if task.times > 0 {
				task.times--
			}
			tw.addTask(task)
		}
	}
}

// get the task position
func (tw *TimeWheel) getPositionAndCircle(d time.Duration) (pos int, circle int) {
	delaySeconds := int(d.Seconds())
	intervalSeconds := int(tw.interval.Seconds())
	circle = int(delaySeconds / intervalSeconds / tw.slotNum)
	pos = int(tw.currentPos+delaySeconds/intervalSeconds) % tw.slotNum
	return
}

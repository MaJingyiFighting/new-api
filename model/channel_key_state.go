package model

import (
	"sync"
	"time"
)

// 429 风暴检测参数
const (
	// 滑动窗口长度,用于统计 429 出现频率
	key429StormWindow = 60 * time.Second
	// 窗口内 429 次数超过该阈值即认为进入风暴模式
	key429StormThreshold = 5
	// 风暴模式暂停时长(暂停该 channel 全部 key 的调度)
	key429StormPause = 5 * time.Minute
)

// key429Record 维护单个 channel 的 429 事件时间序列与风暴状态.
type key429Record struct {
	// 滑动窗口内 429 事件时间戳(unix nano)
	timestamps []int64
	// 该 channel 整体进入风暴模式的截止时间(unix 秒)
	stormUntil int64
}

var (
	key429Mu      sync.Mutex
	key429Tracker = make(map[int]*key429Record)
)

// keyPauseKey 是 (channelId, keyIdx) 的复合 key,用于标记被风暴暂停的 key.
type keyPauseKey struct {
	channelId int
	keyIdx    int
}

var (
	keyPauseMu  sync.Mutex
	keyPauseMap = make(map[keyPauseKey]int64)
)

// HandleUpstream429Storm 记录一次上游 429 事件并判断是否进入 429 风暴.
//   - 维护每个 channelId 的滑动窗口(60s)内 429 错误次数
//   - 当 60s 内 429 次数超过阈值(默认 5)时,该 channel 全部 keys 进入风暴暂停窗口(默认 5 分钟)
//   - 返回 true 表示本次调用刚触发风暴模式
//
// 使用包级 mutex-protected map 维护状态,避免污染 ChannelInfo 持久化结构.
func HandleUpstream429Storm(channelId int, channelType int) bool {
	if channelId <= 0 {
		return false
	}
	now := time.Now()
	nowNano := now.UnixNano()
	nowSec := now.Unix()

	key429Mu.Lock()
	rec, ok := key429Tracker[channelId]
	if !ok {
		rec = &key429Record{}
		key429Tracker[channelId] = rec
	}
	// 清理滑动窗口外的旧事件
	cutoff := nowNano - key429StormWindow.Nanoseconds()
	filtered := rec.timestamps[:0]
	for _, t := range rec.timestamps {
		if t >= cutoff {
			filtered = append(filtered, t)
		}
	}
	filtered = append(filtered, nowNano)
	rec.timestamps = filtered

	// 判断是否处于风暴中
	inStorm := rec.stormUntil > nowSec
	stormTriggered := false
	if !inStorm && len(rec.timestamps) >= key429StormThreshold {
		rec.stormUntil = nowSec + int64(key429StormPause.Seconds())
		stormTriggered = true
		inStorm = true
	}
	key429Mu.Unlock()

	if !inStorm {
		return false
	}

	// 将该 channel 的所有 key 加入风暴暂停窗口.
	channel, err := CacheGetChannel(channelId)
	if err == nil && channel != nil && channel.ChannelInfo.IsMultiKey {
		size := channel.ChannelInfo.MultiKeySize
		if size <= 0 {
			size = len(channel.GetKeys())
		}
		pauseUntil := nowSec + int64(key429StormPause.Seconds())
		keyPauseMu.Lock()
		for i := 0; i < size; i++ {
			keyPauseMap[keyPauseKey{channelId: channelId, keyIdx: i}] = pauseUntil
		}
		keyPauseMu.Unlock()
	}

	return stormTriggered
}

// IsKeyStormPaused 判断指定 key 是否处于 429 风暴暂停窗口内.
// 该函数由 GetNextEnabledKey 在 load_aware 调度时调用,以过滤被风暴暂停的 key.
func IsKeyStormPaused(channelId int, keyIdx int, nowSec int64) bool {
	if channelId <= 0 || keyIdx < 0 {
		return false
	}
	keyPauseMu.Lock()
	defer keyPauseMu.Unlock()
	k := keyPauseKey{channelId: channelId, keyIdx: keyIdx}
	until, ok := keyPauseMap[k]
	if !ok {
		return false
	}
	if nowSec >= until {
		delete(keyPauseMap, k)
		return false
	}
	return true
}

// GetKey429StormCount 获取指定 channel 当前的 429 风暴计数(滑动窗口内 429 事件数).
// 主要用于测试和监控.
func GetKey429StormCount(channelId int) int {
	if channelId <= 0 {
		return 0
	}
	now := time.Now().UnixNano()
	cutoff := now - key429StormWindow.Nanoseconds()
	key429Mu.Lock()
	defer key429Mu.Unlock()
	rec, ok := key429Tracker[channelId]
	if !ok {
		return 0
	}
	// 顺带清理过期事件,使返回值反映真实窗口.
	filtered := rec.timestamps[:0]
	for _, t := range rec.timestamps {
		if t >= cutoff {
			filtered = append(filtered, t)
		}
	}
	rec.timestamps = filtered
	return len(filtered)
}

// IsChannelIn429Storm 判断指定 channel 当前是否处于 429 风暴模式.
func IsChannelIn429Storm(channelId int) bool {
	if channelId <= 0 {
		return false
	}
	now := time.Now().Unix()
	key429Mu.Lock()
	defer key429Mu.Unlock()
	rec, ok := key429Tracker[channelId]
	if !ok {
		return false
	}
	return rec.stormUntil > now
}

// ResetChannel429Storm 重置指定 channel 的 429 风暴状态. 主要用于测试.
func ResetChannel429Storm(channelId int) {
	key429Mu.Lock()
	delete(key429Tracker, channelId)
	key429Mu.Unlock()
	keyPauseMu.Lock()
	for k := range keyPauseMap {
		if k.channelId == channelId {
			delete(keyPauseMap, k)
		}
	}
	keyPauseMu.Unlock()
}

package model

import "testing"

func TestInitChannelCacheCreatesMissingGroupsAndTrimsValues(t *testing.T) {
	defer setupIsolatedDB(t)()

	channel := &Channel{
		Id:     1,
		Status: ChannelStatusEnabled,
		Models: "gpt-4, gpt-4 ",
		Group:  "alpha, beta",
		Name:   "cache-test",
	}
	if err := DB.Create(channel).Error; err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	// Only one group has an Ability. The other group used to cause a nil-map
	// assignment during cache initialization.
	if err := DB.Create(&Ability{Group: "alpha", Model: "gpt-4", ChannelId: channel.Id}).Error; err != nil {
		t.Fatalf("seed ability: %v", err)
	}

	InitChannelCache()

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()
	if got := len(group2model2channels["alpha"]["gpt-4"]); got != 1 {
		t.Fatalf("alpha/gpt-4 cache entries = %d, want 1", got)
	}
	if got := len(group2model2channels["beta"]["gpt-4"]); got != 1 {
		t.Fatalf("beta/gpt-4 cache entries = %d, want 1", got)
	}
	if _, ok := group2model2channels["alpha"]["gpt-4 "]; ok {
		t.Fatal("cache contains an untrimmed model key")
	}
}

package channelrouter

import (
	"context"
	"testing"

	"github.com/modelbus/one-api-pro/model"
)

func TestExcludedChannelFilter(t *testing.T) {
	candidates := []*model.Channel{{Id: 1}, {Id: 2}, {Id: 3}}
	got := (&ExcludedChannelFilter{}).Filter(context.Background(), candidates, &RouteRequest{ExcludedChannelId: 2})
	if len(got) != 2 || got[0].Id != 1 || got[1].Id != 3 {
		t.Fatalf("Filter() = %#v", got)
	}
}

package speaker

import "testing"

// TestDisplayName_PrioritizesCurrentSpeakerProjection 验证所有页面共享同一展示优先级。
func TestDisplayName_PrioritizesCurrentSpeakerProjection(t *testing.T) {
	tests := []struct {
		name        string
		participant string
		clusterNo   int
		trackNo     int
		want        string
	}{
		{name: "正式成员", participant: " 刘志勇 ", clusterNo: 2, trackNo: 3, want: "刘志勇"},
		{name: "未知聚类", clusterNo: 2, trackNo: 3, want: "未知说话人 2"},
		{name: "待识别轨道", trackNo: 3, want: "说话人 3"},
		{name: "供应商无标签", want: "未识别说话人"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := DisplayName(test.participant, test.clusterNo, test.trackNo); got != test.want {
				t.Fatalf("说话人展示错误：got=%q want=%q", got, test.want)
			}
		})
	}
}

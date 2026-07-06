package handler

import (
	"net/http"
	"strconv"

	"botwapp/internal/instance"
	"botwapp/store/postgres"

	"github.com/gin-gonic/gin"
)

type waSyncData struct {
	InstanceID   string
	InstanceName string
	Total        int
	Individual   int
	Groups       int
	SyncedAt     string
	SyncType     string
}

func readWASyncStats(companyID string) []waSyncData {
	toInt := func(s string) int { n, _ := strconv.Atoi(s); return n }
	var result []waSyncData
	for _, inst := range instance.Global.GetByCompany(companyID) {
		total, _ := postgres.GetSetting("wa_sync:" + inst.ID + ":total")
		ind, _ := postgres.GetSetting("wa_sync:" + inst.ID + ":individual")
		grp, _ := postgres.GetSetting("wa_sync:" + inst.ID + ":groups")
		at, _ := postgres.GetSetting("wa_sync:" + inst.ID + ":at")
		tp, _ := postgres.GetSetting("wa_sync:" + inst.ID + ":type")
		result = append(result, waSyncData{
			InstanceID:   inst.ID,
			InstanceName: inst.Name,
			Total:        toInt(total),
			Individual:   toInt(ind),
			Groups:       toInt(grp),
			SyncedAt:     at,
			SyncType:     tp,
		})
	}
	return result
}

func APISyncStats(c *gin.Context) {
	stats := readWASyncStats(currentCompanyID(c))

	type InstStat struct {
		InstanceID   string `json:"instance_id"`
		InstanceName string `json:"instance_name"`
		Total        int    `json:"total"`
		Individual   int    `json:"individual"`
		Groups       int    `json:"groups"`
		SyncedAt     string `json:"synced_at"`
		SyncType     string `json:"sync_type"`
	}

	result := make([]InstStat, 0, len(stats))
	for _, s := range stats {
		result = append(result, InstStat{
			InstanceID:   s.InstanceID,
			InstanceName: s.InstanceName,
			Total:        s.Total,
			Individual:   s.Individual,
			Groups:       s.Groups,
			SyncedAt:     s.SyncedAt,
			SyncType:     s.SyncType,
		})
	}
	c.JSON(http.StatusOK, result)
}

func APISyncExport(c *gin.Context) {
	companyID := currentCompanyID(c)

	insts := instance.Global.GetByCompany(companyID)
	if len(insts) == 0 {
		c.JSON(http.StatusOK, []struct{}{})
		return
	}

	target := insts[0]
	for _, inst := range insts {
		if inst.GetStatus() == "connected" {
			target = inst
			break
		}
	}

	data, err := postgres.GetWASyncCrossRef(target.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, data)
}

func WebSync(c *gin.Context) {
	token, _ := c.Get("token")
	username, _ := c.Get("username")

	type SyncView struct {
		InstanceName string
		Total        int
		Individual   int
		Groups       int
		SyncedAt     string
		SyncType     string
	}

	views := make([]SyncView, 0)
	for _, s := range readWASyncStats(currentCompanyID(c)) {
		views = append(views, SyncView{
			InstanceName: s.InstanceName,
			Total:        s.Total,
			Individual:   s.Individual,
			Groups:       s.Groups,
			SyncedAt:     s.SyncedAt,
			SyncType:     s.SyncType,
		})
	}

	render(c, http.StatusOK, "sync.html", gin.H{
		"Token":    token,
		"Username": username,
		"Role":     currentRole(c),
		"Stats":    views,
	})
}

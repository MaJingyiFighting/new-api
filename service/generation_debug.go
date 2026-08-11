package service

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/generationdebug"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

func mergeGenerationDebugIntoLogOther(
	c *gin.Context,
	other *model.LogOther,
	usage *dto.Usage,
	meta generationdebug.LogMeta,
) {
	if other == nil {
		return
	}

	values := make(map[string]any)
	generationdebug.MergeContextIntoLogOther(c, values, usage, meta)
	if summary, ok := values["generation_debug"]; ok {
		other.SetPublic("generation_debug", summary)
	}
	if adminInfo, ok := values["admin_info"].(map[string]any); ok {
		other.MergeAdmin(adminInfo)
	}
}

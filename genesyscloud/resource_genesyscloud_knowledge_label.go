package genesyscloud

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"terraform-provider-genesyscloud/genesyscloud/consistency_checker"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/mypurecloud/platform-client-sdk-go/v99/platformclientv2"
)

var (
	knowledgeLabel = &schema.Resource{
		Schema: map[string]*schema.Schema{
			"name": {
				Description: "The name of the label.",
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
			},
			"color": {
				Description: "The color for the label.",
				Type:        schema.TypeString,
				Required:    true,
			},
		},
	}
)

func getAllKnowledgeLabels(_ context.Context, clientConfig *platformclientv2.Configuration) (ResourceIDMetaMap, diag.Diagnostics) {
	knowledgeBaseList := make([]platformclientv2.Knowledgebase, 0)
	resources := make(ResourceIDMetaMap)
	knowledgeAPI := platformclientv2.NewKnowledgeApiWithConfig(clientConfig)

	for pageNum := 1; ; pageNum++ {
		const pageSize = 100
		unpublishedKnowledgeBases, _, getErr := knowledgeAPI.GetKnowledgeKnowledgebases("", "", "", fmt.Sprintf("%v", pageSize), "", "", false, "", "")
		if getErr != nil {
			return nil, diag.Errorf("Failed to get page of knowledge bases: %v", getErr)
		}

		publishedKnowledgeBases, _, getErr := knowledgeAPI.GetKnowledgeKnowledgebases("", "", "", fmt.Sprintf("%v", pageSize), "", "", true, "", "")
		if getErr != nil {
			return nil, diag.Errorf("Failed to get page of knowledge bases: %v", getErr)
		}

		if unpublishedKnowledgeBases != nil && len(*unpublishedKnowledgeBases.Entities) > 0 {
			for _, knowledgeBase := range *unpublishedKnowledgeBases.Entities {
				knowledgeBaseList = append(knowledgeBaseList, knowledgeBase)
			}
		}
		if publishedKnowledgeBases != nil && len(*publishedKnowledgeBases.Entities) > 0 {
			for _, knowledgeBase := range *publishedKnowledgeBases.Entities {
				knowledgeBaseList = append(knowledgeBaseList, knowledgeBase)
			}
		}
	}
	for _, knowledgeBase := range knowledgeBaseList {
		for pageNum := 1; ; pageNum++ {
			const pageSize = 100
			knowledgeLabels, _, getErr := knowledgeAPI.GetKnowledgeKnowledgebaseLabels(*knowledgeBase.Id, "", "", fmt.Sprintf("%v", pageSize), "", false)
			if getErr != nil {
				return nil, diag.Errorf("Failed to get page of knowledge labels: %v", getErr)
			}

			if knowledgeLabels.Entities == nil || len(*knowledgeLabels.Entities) == 0 {
				break
			}

			for _, knowledgeLabel := range *knowledgeLabels.Entities {
				id := fmt.Sprintf("%s,%s", *knowledgeLabel.Id, *knowledgeBase.Id)
				resources[id] = &ResourceMeta{Name: *knowledgeLabel.Name}
			}
		}
	}

	return resources, nil
}

func knowledgeLabelExporter() *ResourceExporter {
	return &ResourceExporter{
		GetResourcesFunc: getAllWithPooledClient(getAllKnowledgeCategories),
		RefAttrs: map[string]*RefAttrSettings{
			"knowledge_base_id": {RefType: "genesyscloud_knowledge_knowledgebase"},
		},
	}
}

func resourceKnowledgeLabel() *schema.Resource {
	return &schema.Resource{
		Description: "Genesys Cloud Knowledge Label",

		CreateContext: createWithPooledClient(createKnowledgeLabel),
		ReadContext:   readWithPooledClient(readKnowledgeLabel),
		UpdateContext: updateWithPooledClient(updateKnowledgeLabel),
		DeleteContext: deleteWithPooledClient(deleteKnowledgeLabel),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},
		SchemaVersion: 1,
		Schema: map[string]*schema.Schema{
			"knowledge_base_id": {
				Description: "Knowledge base id of the label",
				Type:        schema.TypeString,
				Required:    true,
			},
			"knowledge_label": {
				Description: "Knowledge label id",
				Type:        schema.TypeList,
				MaxItems:    1,
				Required:    true,
				Elem:        knowledgeLabel,
			},
		},
	}
}

func createKnowledgeLabel(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	knowledgeBaseId := d.Get("knowledge_base_id").(string)
	knowledgeLabel := d.Get("knowledge_label").([]interface{})[0].(map[string]interface{})

	sdkConfig := meta.(*ProviderMeta).ClientConfig
	knowledgeAPI := platformclientv2.NewKnowledgeApiWithConfig(sdkConfig)

	knowledgeLabelRequest := buildKnowledgeLabel(knowledgeLabel)

	log.Printf("Creating knowledge label %s", knowledgeLabel["name"].(string))
	knowledgeLabelResponse, _, err := knowledgeAPI.PostKnowledgeKnowledgebaseLabels(knowledgeBaseId, knowledgeLabelRequest)
	if err != nil {
		return diag.Errorf("Failed to create knowledge label %s: %s", knowledgeBaseId, err)
	}

	id := fmt.Sprintf("%s,%s", *knowledgeLabelResponse.Id, knowledgeBaseId)
	d.SetId(id)

	log.Printf("Created knowledge label %s", *knowledgeLabelResponse.Id)
	return readKnowledgeLabel(ctx, d, meta)
}

func readKnowledgeLabel(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	id := strings.Split(d.Id(), ",")
	knowledgeLabelId := id[0]
	knowledgeBaseId := id[1]

	sdkConfig := meta.(*ProviderMeta).ClientConfig
	knowledgeAPI := platformclientv2.NewKnowledgeApiWithConfig(sdkConfig)

	log.Printf("Reading knowledge label %s", knowledgeLabelId)
	return withRetriesForRead(ctx, d, func() *resource.RetryError {
		knowledgeLabel, resp, getErr := knowledgeAPI.GetKnowledgeKnowledgebaseLabel(knowledgeBaseId, knowledgeLabelId)
		if getErr != nil {
			if isStatus404(resp) {
				return resource.RetryableError(fmt.Errorf("Failed to read knowledge label %s: %s", knowledgeLabelId, getErr))
			}
			log.Printf("%s", getErr)
			return resource.NonRetryableError(fmt.Errorf("Failed to read knowledge label %s: %s", knowledgeLabelId, getErr))
		}

		cc := consistency_checker.NewConsistencyCheck(ctx, d, meta, resourceKnowledgeLabel())

		newId := fmt.Sprintf("%s,%s", *knowledgeLabel.Id, knowledgeBaseId)
		d.SetId(newId)
		d.Set("knowledge_base_id", knowledgeBaseId)
		d.Set("knowledge_label", flattenKnowledgeLabel(knowledgeLabel))
		log.Printf("Read knowledge label %s", knowledgeLabelId)
		return cc.CheckState()
	})
}

func updateKnowledgeLabel(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	id := strings.Split(d.Id(), ",")
	knowledgeLabelId := id[0]
	knowledgeBaseId := id[1]
	knowledgeLabel := d.Get("knowledge_label").([]interface{})[0].(map[string]interface{})

	sdkConfig := meta.(*ProviderMeta).ClientConfig
	knowledgeAPI := platformclientv2.NewKnowledgeApiWithConfig(sdkConfig)

	log.Printf("Updating knowledge label %s", knowledgeLabel["name"].(string))
	diagErr := retryWhen(isVersionMismatch, func() (*platformclientv2.APIResponse, diag.Diagnostics) {
		// Get current knowledge label version
		_, resp, getErr := knowledgeAPI.GetKnowledgeKnowledgebaseLabel(knowledgeBaseId, knowledgeLabelId)
		if getErr != nil {
			return resp, diag.Errorf("Failed to read knowledge label %s: %s", knowledgeLabelId, getErr)
		}

		knowledgeLabelUpdate := buildKnowledgeLabelUpdate(knowledgeLabel)

		log.Printf("Updating knowledge label %s", knowledgeLabel["name"].(string))
		_, resp, putErr := knowledgeAPI.PatchKnowledgeKnowledgebaseLabel(knowledgeBaseId, knowledgeLabelId, knowledgeLabelUpdate)
		if putErr != nil {
			return resp, diag.Errorf("Failed to update knowledge label %s: %s", knowledgeLabelId, putErr)
		}
		return resp, nil
	})
	if diagErr != nil {
		return diagErr
	}

	log.Printf("Updated knowledge label %s %s", knowledgeLabel["name"].(string), knowledgeLabelId)
	return readKnowledgeLabel(ctx, d, meta)
}

func deleteKnowledgeLabel(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	id := strings.Split(d.Id(), ",")
	knowledgeLabelId := id[0]
	knowledgeBaseId := id[1]

	sdkConfig := meta.(*ProviderMeta).ClientConfig
	knowledgeAPI := platformclientv2.NewKnowledgeApiWithConfig(sdkConfig)

	log.Printf("Deleting knowledge label %s", id)
	_, _, err := knowledgeAPI.DeleteKnowledgeKnowledgebaseLabel(knowledgeBaseId, knowledgeLabelId)
	if err != nil {
		return diag.Errorf("Failed to delete knowledge label %s: %s", id, err)
	}

	return withRetries(ctx, 30*time.Second, func() *resource.RetryError {
		_, resp, err := knowledgeAPI.GetKnowledgeKnowledgebaseLabel(knowledgeBaseId, knowledgeLabelId)
		if err != nil {
			if isStatus404(resp) {
				// Knowledge label deleted
				log.Printf("Deleted knowledge label %s", knowledgeLabelId)
				return nil
			}
			return resource.NonRetryableError(fmt.Errorf("Error deleting knowledge label %s: %s", knowledgeLabelId, err))
		}

		return resource.RetryableError(fmt.Errorf("Knowledge label %s still exists", knowledgeLabelId))
	})
}

func buildKnowledgeLabel(labelIn map[string]interface{}) platformclientv2.Labelcreaterequest {
	name := labelIn["name"].(string)
	color := labelIn["color"].(string)

	labelOut := platformclientv2.Labelcreaterequest{
		Name:  &name,
		Color: &color,
	}

	return labelOut
}

func buildKnowledgeLabelUpdate(labelIn map[string]interface{}) platformclientv2.Labelupdaterequest {
	name := labelIn["name"].(string)
	color := labelIn["color"].(string)

	labelOut := platformclientv2.Labelupdaterequest{
		Name:  &name,
		Color: &color,
	}

	return labelOut
}

func flattenKnowledgeLabel(labelIn *platformclientv2.Labelresponse) []interface{} {
	labelOut := make(map[string]interface{})

	if labelIn.Name != nil {
		labelOut["name"] = *labelIn.Name
	}
	if labelIn.Color != nil {
		labelOut["color"] = *labelIn.Color
	}

	return []interface{}{labelOut}
}

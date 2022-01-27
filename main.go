package main

import (
	"bytes"
	"os"
	"text/template"

	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/support"
)

/**
 * Uploads NVidia logs to AWS Support.
 * Returns:
 *   error: Err in process of uploading the logs
 *   string: AttachmentSetId
 */
func uploadLogs(client *support.Support, nvidialogs *[]string) (string, error) {
	ats := support.AddAttachmentsToSetInput{}
	atmnts := make([]*support.Attachment, len(nvidialogs))
	for i, fp := range nvidialogs {
		atmnt := new(support.Attachment)
		dat, err := os.ReadFile(fp)
		if err != nil {
			return "", err
		}
		atmnt.SetData(dat)
		atmnt.SetFileName(fp) // TODO(?): put generated proper filename
		atmnts[i] = atmnt

	}
	ats.SetAttachments(atmnts)
	resp, err := client.AddAttachmentsToSet(ats)
	if err != nil {
		return "", err
	}
	return nil, resp.AttachmentSetId
}

/**
 * Creates a support case to request cordoning an ec2 instance
 * Returns:
 *   error: Erro in process of creating the case
 *   string: Created case's CaseId
 */
func RequestNodeCordon(nvidialogs *[]string) (string, error) {
	mySession := session.Must(session.NewSession())
	client := support.New(mySession)

	atsId, err := uploadLogs(client, nvidialogs)
	if err != nil {
		return "", err
	}

	tmpl, err := template.New("body").Parse(`
 Please cordon of ec2 instance:
 Instance Id: {{.InstanceID}}
 Region: {{.Region}} 
 AZ: {{.AvailabilityZone}}
 Instance Type: {{.InstanceType}}
 Account Id: {{.AccountID}}
 Image Id: {{.ImageID}}
 Kernel Id: {{.KernelID}}

 Attached are Nvidia Bug report logs.

 Refer to Failure Modes - Require Degrade of the underlying EC2 Host
 in the support playbook for more info
 `)
	if err != nil {
		return "", err
	}

	mdClient := ec2metadata.New(mySession)
	iid, err := mdClient.GetInstanceIdentityDocument()
	if err != nil {
		return "", err
	}
	var body bytes.Buffer
	err := tmp.Execute(body, iid)
	if err != nil {
		return "", err
	}

	var supportCase = support.CreateCaseInput{}
	supportCase.SetCcEmailAddresses([]string{"bolt-aws@apple.com", "..."})
	supportCase.SetSubject("GPU Faults/Errors Encountered | Hardware Cordon Requested")
	supportCase.SetCommunicationBody(body.String())
	supportCase.SetIssueType("technical")
	supportCase.SetLanguage("en")

	// Not sure on this one, maybe ec2
	// Can get list of codes by running:
	// aws support describe-services --region=us-east-1
	supportCase.SetServiceCode("amazon-elastic-compute-cloud-linux instance-issue")
	supportCase.SetSeverityCode("high")
	supportCase.SetAttachmentSetId(atsId)

	if err := supportCase.Validate(); err != nil {
		return "", err
	}

	res, err := client.CreateCase(supportCase)
	if err != nil {
		return "", err
	}
	return res.CaseId, nil

}

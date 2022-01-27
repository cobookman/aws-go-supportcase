package main

/**
 * Support SDK Godocs: https://docs.aws.amazon.com/sdk-for-go/api/service/support
 * Support Go Src code: https://github.com/aws/aws-sdk-go/blob/37a82efacad413c32032d9e120bc84ae54162164/service/support/api.go#L1514
 */

import (
	"errors"
	"bytes"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/support"
	"os"
	"text/template"
)

var (
	CC_EMAIL_1         = "foo@foo.com"
	CC_EMAIL_2         = "baz@baz.com"
	CC_EMAILS          = []*string{&CC_EMAIL_1, &CC_EMAIL_2}
	CASE_BODY_TEMPLATE = `
Please cordon off the following ec2 instance:
  * Instance Id: {{.InstanceID}}
  * Region: {{.Region}} 
  * AZ: {{.AvailabilityZone}}
  * Instance Type: {{.InstanceType}}
  * Account Id: {{.AccountID}}
  * Image Id: {{.ImageID}}
  * Kernel Id: {{.KernelID}}
   
Attached are Nvidia Bug report logs.
   
Refer to Failure Modes - Require Degrade of the underlying EC2 Host
in the support playbook for more info`
)

/**
 * Uploads NVidia logs to AWS Support.
 * Returns:
 *   error: Err in process of uploading the logs
 *   string: AttachmentSetId
 */
func uploadLogs(client *support.Support, nvidialogs []string) (string, error) {
	ats := new(support.AddAttachmentsToSetInput)
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
	return *resp.AttachmentSetId, nil
}

/**
 * Uses Ec2 Metadata API to enrich a support case body with instance details.
 * Returns:
 *   string: Case body text
 *   error: Errors generated in process of calling Ec2 API or text/template
 */
func genCaseBody(session *session.Session) (string, error) {
	// Populate case body with metadata on Ec2 instance
	client := ec2metadata.New(session)
	if !client.Available() {
		return "", errors.New("Cannot connect to ec2 metadata service")
	}

	iid, err := client.GetInstanceIdentityDocument()
	if err != nil {
		return "", err
	}

	tmpl, err := template.New("body").Parse(CASE_BODY_TEMPLATE)
	if err != nil {
		return "", err
	}

	var body bytes.Buffer
	if err := tmpl.Execute(&body, iid); err != nil {
		return "", err
	}
	return body.String(), nil
}

/**
 * Creates a support case to request cordoning an ec2 instance
 * Returns:
 *   string: Created case's CaseId
 *   error: errors in process of creating the case
 */
func RequestNodeCordon(nvidialogs []string) (string, error) {
	mySession := session.Must(session.NewSession())

	// Support's API is only available in us-east-1
	// See: https://docs.aws.amazon.com/general/latest/gr/awssupport.html
	client := support.New(mySession, aws.NewConfig().WithRegion("us-east-1"))
	atsId, err := uploadLogs(client, nvidialogs)
	if err != nil {
		return "", err
	}

	body, err := genCaseBody(mySession)
	if err != nil {
		return "", err
	}
	fmt.Println(body)

	// Populate case fields
	supportCase := new(support.CreateCaseInput)
	supportCase.SetCcEmailAddresses(CC_EMAILS)
	supportCase.SetSubject("GPU Faults/Errors Encountered | Hardware Cordon Requested")
	supportCase.SetCommunicationBody(body)
	supportCase.SetIssueType("technical")
	supportCase.SetLanguage("en")

	// A list of support case service codes & category codes can be found using the CLI:
	// $ aws support describe-services --region=us-east-1
	supportCase.SetServiceCode("amazon-elastic-compute-cloud-linux")
	supportCase.SetCategoryCode("instance-issue")

	// A list of suport case severity levels and associated code can be found using the CLI:
	// $ aws support describe-severity-levels
	supportCase.SetSeverityCode("urgent")
	supportCase.SetAttachmentSetId(atsId)

	if err := supportCase.Validate(); err != nil {
		return "", err
	}

	res, err := client.CreateCase(supportCase)
	if err != nil {
		return "", err
	}
	return *res.CaseId, nil

}

func main() {
	caseID, err := RequestNodeCordon([]string{"./nvidia-bug-report.log.gz"})
	if err != nil {
		panic(err)
	}
	fmt.Printf("Case Id: %s\n", caseID)
}

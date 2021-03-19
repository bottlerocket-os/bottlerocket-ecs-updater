package main

import (
	"flag"
	"log"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecs"
)

var (
	flagCluster = flag.String("cluster", "", "The short name or full Amazon Resource Name (ARN) of the cluster in which we will manage Bottlerocket instances.")
	flagRegion  = flag.String("region", "", "The AWS Region in which cluster is running.")
)

func main() {
	flag.Parse()
	switch {
	case *flagCluster == "":
		log.Println("cluster is required")
		flag.Usage()
		os.Exit(1)
	case *flagRegion == "":
		log.Println("region is required")
		flag.Usage()
		os.Exit(1)
	}
	// Using default credential Provider; it looks for credentials in following order:
	// 1. Environment variables.
	// 2. Shared credentials file.
	// 3. If application uses an ECS task definition or RunTask API operation, IAM role for tasks.
	//
	// default credential Provider best suits our requirement where we can use shared cred files for local run
	// and IAM role for Fargate task
	sess := session.Must(session.NewSession(&aws.Config{
		Region: aws.String(*flagRegion),
	}))
	ecsClient := ecs.New(sess)
	resp, err := ecsClient.ListContainerInstances(&ecs.ListContainerInstancesInput{Cluster: flagCluster})
	if err != nil {
		log.Printf("Cannot list container Instances: %#v", err)
		os.Exit(1)
	}
	log.Printf("List of container Instances %#v", resp)
}

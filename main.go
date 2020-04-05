package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

var (
	profile       string
	verbose       bool
	sess          client.ConfigProvider
	config        aws.Config
	defaultRegion string = "ap-northeast-1"
	costDef       map[string]float64
)

// Bucket ... usage by bucket
type Bucket struct {
	Name            string
	Region          string
	NumberOfObjects float64
	TotalSize       float64
	TotalCost       float64
	Sizes           map[string]float64
	Costs           map[string]float64
}

func init() {
	flag.StringVar(&profile, "p", "default", "aws shared credential profile name")
	flag.BoolVar(&verbose, "v", false, "show detail cost, if enabled")
	flag.Parse()
	config = aws.Config{
		Credentials: credentials.NewSharedCredentials("", profile),
	}
	sess = session.Must(session.NewSession())

	// tokyo region cost
	costDef = map[string]float64{
		"StandardStorage":             0.025,
		"IntelligentTieringStorage":   0.025,
		"StandardIAStorage":           0.019,
		"StandardIASizeOverhead":      0.019,
		"StandardIAObjectOverhead":    0.019,
		"OneZoneIAStorage":            0.0152,
		"OneZoneIASizeOverhead":       0.0152,
		"ReducedRedundancyStorage":    0.0259,
		"GlacierStorage":              0.005,
		"GlacierStagingStorage":       0.005,
		"GlacierObjectOverhead":       0.005,
		"GlacierS3ObjectOverhead":     0.025,
		"DeepArchiveStorage":          0.002,
		"DeepArchiveObjectOverhead":   0.002,
		"DeepArchiveS3ObjectOverhead": 0.025,
		"DeepArchiveStagingStorage":   0.002,
	}
}

func main() {
	var wg sync.WaitGroup
	var mu sync.Mutex

	fmt.Println(" ObjectCount      GigaBytes    Charges-USD  BucketName (Region)")
	bucketNames := getBucketNames()
	limiter := make(chan int, 20)
	for _, bucketName := range bucketNames {
		limiter <- 1
		wg.Add(1)
		go func(bucketName string) {
			defer func() {
				<-limiter
				wg.Done()
			}()
			bucket := Bucket{
				Name:   bucketName,
				Region: getRegion(bucketName),
				Sizes:  map[string]float64{},
				Costs:  map[string]float64{},
			}
			bucket.NumberOfObjects = getNumberOfObjects(bucket)
			for storageType, costGbMonth := range costDef {
				tmpBytes := getBucketSizeGB(bucket, storageType)
				bucket.Sizes[storageType] = tmpBytes
				bucket.TotalSize += tmpBytes
				bucket.Costs[storageType] = tmpBytes * costGbMonth
				bucket.TotalCost += tmpBytes * costGbMonth
			}
			mu.Lock()
			defer mu.Unlock()

			// show result
			fmt.Printf("%12d %14.2f %14.2f  %s (%s)\n",
				int(bucket.NumberOfObjects),
				bucket.TotalSize,
				bucket.TotalCost,
				bucket.Name,
				bucket.Region)
			if verbose {
				for storageType := range costDef {
					if bucket.Sizes[storageType] != 0.0 {
						fmt.Printf(" %26.2f %14.2f   - %s\n",
							bucket.Sizes[storageType],
							bucket.Costs[storageType],
							storageType)
					}
				}
				fmt.Println()
			}
		}(bucketName)
	}
	wg.Wait()
}

func getBucketNames() []string {
	bcuketNames := []string{}
	s3Svc := s3.New(sess, config.WithRegion(defaultRegion))
	resp, _ := s3Svc.ListBuckets(nil)
	for _, b := range resp.Buckets {
		bcuketNames = append(bcuketNames, *b.Name)
	}
	return bcuketNames
}

func getRegion(bucketName string) string {
	region, err := s3manager.GetBucketRegion(context.Background(), sess, bucketName, defaultRegion)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok && aerr.Code() == "NotFound" {
			fmt.Fprintf(os.Stderr, "unable to find bucket %s's region not found\n", bucketName)
		}
	}
	return region
}

func getNumberOfObjects(bucket Bucket) float64 {
	params := &cloudwatch.GetMetricStatisticsInput{
		StartTime:  aws.Time(time.Now().Add(time.Duration(24) * time.Hour * -2)),
		EndTime:    aws.Time(time.Now()),
		MetricName: aws.String("NumberOfObjects"),
		Namespace:  aws.String("AWS/S3"),
		Period:     aws.Int64(86400),
		Statistics: []*string{aws.String(cloudwatch.StatisticAverage)},
		Dimensions: []*cloudwatch.Dimension{
			{
				Name:  aws.String("BucketName"),
				Value: aws.String(bucket.Name),
			},
			{
				Name:  aws.String("StorageType"),
				Value: aws.String("AllStorageTypes"),
			},
		},
		Unit: aws.String(cloudwatch.StandardUnitCount),
	}

	cwSvc := cloudwatch.New(sess, config.WithRegion(bucket.Region))
	resp, _ := cwSvc.GetMetricStatistics(params)
	sort.Slice(resp.Datapoints, func(i, j int) bool {
		return resp.Datapoints[i].Timestamp.Unix() > resp.Datapoints[j].Timestamp.Unix()
	})
	if resp.Datapoints != nil {
		return *resp.Datapoints[0].Average
	}
	return 0.0
}

func getBucketSizeGB(bucket Bucket, storageType string) float64 {
	params := &cloudwatch.GetMetricStatisticsInput{
		StartTime:  aws.Time(time.Now().Add(time.Duration(24) * time.Hour * -3)),
		EndTime:    aws.Time(time.Now()),
		MetricName: aws.String("BucketSizeBytes"),
		Namespace:  aws.String("AWS/S3"),
		Period:     aws.Int64(86400),
		Statistics: []*string{aws.String(cloudwatch.StatisticAverage)},
		Dimensions: []*cloudwatch.Dimension{
			{
				Name:  aws.String("BucketName"),
				Value: aws.String(bucket.Name),
			},
			{
				Name:  aws.String("StorageType"),
				Value: aws.String(storageType),
			},
		},
		Unit: aws.String(cloudwatch.StandardUnitBytes),
	}

	cwSvc := cloudwatch.New(sess, config.WithRegion(bucket.Region))
	resp, _ := cwSvc.GetMetricStatistics(params)
	sort.Slice(resp.Datapoints, func(i, j int) bool {
		return resp.Datapoints[i].Timestamp.Unix() > resp.Datapoints[j].Timestamp.Unix()
	})
	if resp.Datapoints != nil {
		return *resp.Datapoints[0].Average / 1024 / 1024 / 1024
	}
	return 0.0
}

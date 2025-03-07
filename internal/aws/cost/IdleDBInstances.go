package cost

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdsTypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/brittandeyoung/ckia/internal/client"
	"github.com/brittandeyoung/ckia/internal/common"
)

const (
	IdleDBInstancesCheckId                  = "ckia:aws:cost:IdleDBInstances"
	IdleDBInstancesCheckName                = "RDS Idle DB Instances"
	IdleDBInstancesCheckDescription         = "Checks the configuration of your Amazon Relational Database Service (Amazon RDS) for any DB instances that appear to be idle. If a DB instance has not had a connection for a prolonged period of time, you can delete the instance to reduce costs. If persistent storage is needed for data on the instance, you can use lower-cost options such as taking and retaining a DB snapshot. Manually created DB snapshots are retained until you delete them."
	IdleDBInstancesCheckCriteria            = "Any RDS DB instance that has not had a connection in the last 7 days is considered idle."
	IdleDBInstancesCheckRecommendedAction   = "Consider taking a snapshot of the idle DB instance and then either stopping it or deleting it. Stopping the DB instance removes some of the costs for it, but does not remove storage costs. A stopped instance keeps all automated backups based upon the configured retention period. Stopping a DB instance usually incurs additional costs when compared to deleting the instance and then retaining only the final snapshot."
	IdleDBInstancesCheckAdditionalResources = "See comparable AWS Trusted advisor check: https://docs.aws.amazon.com/awssupport/latest/user/cost-optimization-checks.html#amazon-rds-idle-dbs-instances"
)

type IdleDBInstance struct {
	Region                  string `json:"region"`
	DBInstanceName          string `json:"dbInstanceName"`
	MultiAZ                 bool   `json:"multiAZ"`
	InstanceType            string `json:"instanceType"`
	StorageProvisionedInGB  int    `json:"storageProvisionedInGB"`
	DaysSinceLastConnection int    `json:"daysSinceLastConnection"`
	EstimatedMonthlySavings int    `json:"estimatedMonthlySavings"`
}

type IdleDBInstancesCheck struct {
	common.Check
	IdleDBInstances []IdleDBInstance `json:"idleDBInstances"`
}

func (v IdleDBInstancesCheck) List() *IdleDBInstancesCheck {
	check := &IdleDBInstancesCheck{
		Check: common.Check{
			Id:                  IdleDBInstancesCheckId,
			Name:                IdleDBInstancesCheckName,
			Description:         IdleDBInstancesCheckDescription,
			Criteria:            IdleDBInstancesCheckCriteria,
			RecommendedAction:   IdleDBInstancesCheckRecommendedAction,
			AdditionalResources: IdleDBInstancesCheckAdditionalResources,
		},
	}
	return check
}

func (v IdleDBInstancesCheck) Run(ctx context.Context, conn client.AWSClient) (*IdleDBInstancesCheck, error) {
	check := new(IdleDBInstancesCheck).List()

	currentTime := time.Now()

	in := &rds.DescribeDBInstancesInput{}
	var dbInstances []rdsTypes.DBInstance

	paginator := rds.NewDescribeDBInstancesPaginator(conn.RDS, in, func(o *rds.DescribeDBInstancesPaginatorOptions) {})

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)

		if err != nil {
			return nil, err
		}
		dbInstances = append(dbInstances, output.DBInstances...)

	}

	if len(dbInstances) == 0 {
		return nil, nil
	}

	var idleDBInstances []IdleDBInstance
	for _, dbInstance := range dbInstances {

		metrics, err := conn.Cloudwatch.GetMetricStatistics(ctx, &cloudwatch.GetMetricStatisticsInput{
			MetricName: aws.String("DatabaseConnections"),
			Period:     aws.Int32(3600),
			Namespace:  aws.String("AWS/RDS"),
			Statistics: []types.Statistic{types.StatisticAverage},
			Dimensions: []types.Dimension{
				{
					Name:  aws.String("DBInstanceIdentifier"),
					Value: dbInstance.DBInstanceIdentifier,
				},
			},
			StartTime: aws.Time(currentTime.AddDate(0, 0, -14)),
			EndTime:   aws.Time(currentTime),
		})

		if err != nil {
			return nil, err
		}

		var idleDBInstance IdleDBInstance
		daysSinceConnection, connectionFound := expandConnections(metrics.Datapoints)

		if !connectionFound {
			// pricingSvc := pricing.NewFromConfig(cfg)
			// filters := []pricingtypes.Filter{
			// 	{
			// 		Field: aws.String("InstanceType"),
			// 		Type:  "TERM_MATCH",
			// 		Value: dbInstance.DBInstanceClass,
			// 	},
			// 	// These two seam to not match what the pricing API is expecting
			// 	{
			// 		Field: aws.String("storage"),
			// 		Type:  "TERM_MATCH",
			// 		Value: dbInstance.StorageType,
			// 	},
			// 	{
			// 		Field: aws.String("databaseEngine"),
			// 		Type:  "TERM_MATCH",
			// 		Value: dbInstance.Engine,
			// 	},
			// 	{
			// 		Field: aws.String("deploymentOption"),
			// 		Type:  "TERM_MATCH",
			// 		Value: aws.String("Single-AZ"),
			// 	},
			// 	{
			// 		Field: aws.String("termType"),
			// 		Type:  "TERM_MATCH",
			// 		Value: aws.String("OnDemand"),
			// 	},
			// 	{
			// 		Field: aws.String("regionCode"),
			// 		Type:  "TERM_MATCH",
			// 		Value: &cfg.Region,
			// 	},
			// 	{
			// 		Field: aws.String("purchaseOption"),
			// 		Type:  "TERM_MATCH",
			// 		Value: aws.String("No Upfront"),
			// 	},
			// }

			// pricingIn := &pricing.GetProductsInput{
			// 	ServiceCode: aws.String("AmazonRDS"),
			// 	Filters:     filters,
			// }
			// pricingData, err := pricingSvc.GetProducts(ctx, pricingIn)

			// if err != nil {
			// 	return IdleDBInstancesCheck{}
			// }

			idleDBInstance.DBInstanceName = aws.ToString(dbInstance.DBInstanceIdentifier)
			idleDBInstance.Region = conn.Region
			idleDBInstance.DaysSinceLastConnection = daysSinceConnection
			idleDBInstance.InstanceType = aws.ToString(dbInstance.DBInstanceClass)
			idleDBInstance.MultiAZ = dbInstance.MultiAZ
			idleDBInstance.StorageProvisionedInGB = int(dbInstance.AllocatedStorage)
			// Still trying to figure out how to get the proper on demand pricing via the API
			// idleDBInstance.EstimatedMonthlySavings = 0
			idleDBInstances = append(idleDBInstances, idleDBInstance)
		}

	}

	check.IdleDBInstances = idleDBInstances
	return check, nil
}

func expandConnections(dataPoints []types.Datapoint) (int, bool) {
	connectionFound := false
	var daysSinceConnection float64
	daysSinceConnection = 14
	for _, dataPoint := range dataPoints {
		if aws.ToFloat64(dataPoint.Average) != 0 {
			duration := time.Now().Sub(aws.ToTime(dataPoint.Timestamp))
			if duration.Hours()/24 < daysSinceConnection {
				daysSinceConnection = duration.Hours() / 24
			}

			if duration.Hours()/24 <= 7 {
				connectionFound = true
			}
		}
	}
	return int(daysSinceConnection), connectionFound
}

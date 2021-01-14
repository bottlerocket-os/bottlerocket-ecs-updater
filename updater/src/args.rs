use structopt::StructOpt;

/// Bottlerocket ECS Updater
///
/// Watches Bottlerocket instances in your ECS cluster, and updates them when they have updates
/// available.
///
/// Arguments can be specified by environment variable. Command-line arguments will override a value
/// that is given by environment variable.
///
#[derive(StructOpt, Debug)]
pub struct Args {
    /// The ARN of the cluster in which we will manage Bottlerocket instances.
    #[structopt(long, env = "BOTTLEROCKET_ECS_UPDATER_CLUSTER_ARN")]
    pub cluster_arn: String,
    /// The AWS Region in which cluster is running
    #[structopt(long, env = "REGION")]
    pub region: String,
}

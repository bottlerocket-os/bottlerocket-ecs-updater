use simplelog::LevelFilter;
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
    /// How much detail to log; from least to most: ERROR, WARN, INFO, DEBUG, TRACE
    #[structopt(long, env = "LOG_LEVEL")]
    pub log_level: LevelFilter,
    /// The ARN of the cluster in which we will manage Bottlerocket instances.
    #[structopt(long, env = "BOTTLEROCKET_ECS_UPDATER_CLUSTER_ARN")]
    pub cluster_name: String,
    /// The AWS Region in which cluster is running
    #[structopt(long, env = "BOTTLEROCKET_ECS_REGION")]
    pub region: String,
}

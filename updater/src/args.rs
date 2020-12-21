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
    /// The name of the cluster in which we will manage Bottlerocket instances.
    #[structopt(long, env = "BOTTLEROCKET_ECS_UPDATER_CLUSTER_NAME")]
    pub cluster_name: String,
}

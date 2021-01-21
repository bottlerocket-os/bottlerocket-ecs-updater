mod args;
mod aws;
mod check;
mod error;

use crate::args::Args;
use crate::aws::api::Mediator;
use aws::api::AwsMediator;
use check::check_updates;
use log::info;
use rusoto_ecs::EcsClient;
use rusoto_ssm::SsmClient;
use simplelog::{Config as LogConfig, SimpleLogger};
use snafu::{ensure, ResultExt};
use std::process;
use structopt::StructOpt;
// Returning a Result from main makes it print a Debug representation of the error, but with Snafu
// we have nice Display representations of the error, so we wrap "main" (run) and print any error.
// https://github.com/shepmaster/snafu/issues/110
#[tokio::main]
async fn main() {
    if let Err(e) = run().await {
        eprintln!("{}", e);
        process::exit(1);
    }
}

async fn run() -> error::Result<()> {
    let args = Args::from_args();
    // Region is required
    ensure!(!args.region.is_empty(), error::EmptyRegion);
    // Log setup
    SimpleLogger::init(args.log_level, LogConfig::default()).context(error::Logger)?;
    info!("bottlerocket-ecs-updater started with {:?}", args);
    let region = aws::region_from_string(&args.region).context(error::InvalidRegion)?;
    let ecs_client = aws::client::build_client::<EcsClient>(&region).context(error::EcsClient)?;
    let ssm_client = aws::client::build_client::<SsmClient>(&region).context(error::SsmClient)?;
    let aws_api = Box::new(AwsMediator::new_with(ecs_client, ssm_client));
    _run(&args, aws_api).await
}

pub async fn _run(args: &Args, aws_api: Box<dyn Mediator>) -> error::Result<()> {
    info!("checking for updates");
    check_updates(&args, aws_api).await
}

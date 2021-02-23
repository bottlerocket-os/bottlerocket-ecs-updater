#![deny(rust_2018_idioms)]
use bottlerocket_ecs_updater::{new_ecs, new_ssm, Args, Result, Updater};
use std::process;
use structopt::StructOpt;

#[tokio::main]
async fn main() {
    let args = Args::from_args();
    // we want to print the error message using the display trait
    if let Err(e) = main_inner(args).await {
        eprintln!("{}", e);
        process::exit(1);
    }
}

pub async fn main_inner(args: Args) -> Result<()> {
    let ecs_mediator = new_ecs(&args.region)?;
    let ssm_mediator = new_ssm(&args.region)?;
    let updater = Updater::new(args, ecs_mediator, ssm_mediator);
    updater.run().await
}

mod args;
use crate::args::Args;
use structopt::StructOpt;

#[tokio::main]
async fn main() {
    let args = Args::from_args();
    println!("{:?}", args)
}

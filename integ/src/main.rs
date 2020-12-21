mod integ_args;
use crate::integ_args::IntegArgs;
use structopt::StructOpt;

fn main() {
    let args = IntegArgs::from_args();
    println!("{:?}", args)
}

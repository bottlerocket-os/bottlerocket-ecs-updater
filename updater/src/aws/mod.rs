/*!
`aws` module creates a wrapper around aws api calls and provides well structured
output containing only the required fields.
*/
use crate::aws::error::Result;
use rusoto_core::Region;
use snafu::ResultExt;
use std::str::FromStr;

pub mod api;
pub(crate) mod client;
pub(crate) mod error;

/// Builds a Region from the given region name, and uses the custom endpoint from the AWS config,
/// if specified in aws.region.REGION.endpoint.
pub(crate) fn region_from_string(name: &str) -> Result<Region> {
    Ok(Region::from_str(name).context(error::ParseRegion { name })?)
}

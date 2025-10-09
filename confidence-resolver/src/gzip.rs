use miniz_oxide::inflate::decompress_to_vec;

use crate::err::{Fallible, OrFailExt};
use crate::fail;

const FHCRC: u8 = 1 << 1;
const FEXTRA: u8 = 1 << 2;
const FNAME: u8 = 1 << 3;
const FCOMMENT: u8 = 1 << 4;
const FRESERVED: u8 = 1 << 5 | 1 << 6 | 1 << 7;

pub fn decompress_gz(buffer: &[u8]) -> Fallible<Vec<u8>> {
    let [m0, m1, cm, flags, ..] = *buffer else {
        fail!();
    };
    // let header : &[u8; 4] = buffer.get(0..4).ok_or("truncated header")?.try_into().map_err(|_| "err")?;
    if m0 != 0x1f || m1 != 0x8b {
        fail!("invalid magic number");
    }
    if cm != 8 {
        fail!("invalid compression method");
    }
    if flags & FRESERVED != 0 {
        fail!("invalid flags");
    }
    if flags & FEXTRA != 0 {
        fail!("extra data not supported");
    }
    if flags & FNAME != 0 {
        fail!("filename not supported");
    }
    if flags & FCOMMENT != 0 {
        fail!("comment not supported");
    }
    if flags & FHCRC != 0 {
        fail!("crc not supported");
    }
    let trailer_start = buffer.len().checked_sub(8).or_fail()?;
    let crc_end = trailer_start.checked_add(4).or_fail()?;
    let isize_end = trailer_start.checked_add(8).or_fail()?;

    let crc_bytes = buffer.get(trailer_start..crc_end).or_fail()?;
    let crc = u32::from_le_bytes(crc_bytes.try_into().or_fail()?);

    let isize_bytes = buffer.get(crc_end..isize_end).or_fail()?;
    let isize = u32::from_le_bytes(isize_bytes.try_into().or_fail()?);

    let compressed_bytes = buffer.get(10..trailer_start).or_fail()?;
    let data = decompress_to_vec(compressed_bytes).or_fail()?;
    if isize != data.len() as u32 {
        fail!("invalid data length");
    }
    let crc_calc = crc32fast::hash(&data);
    if crc_calc != crc {
        fail!("crc mismatch");
    }
    Ok(data)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs::File;
    use std::io::Read;
    use std::println;

    #[test]
    fn test_decompress_gz() {
        let mut file = File::open("test-payloads/bitset.gz").expect("Failed to open test file");
        let mut buffer = Vec::new();
        file.read_to_end(&mut buffer)
            .expect("Failed to read test file");
        let data = decompress_gz(&buffer).expect("Failed to decompress");
        println!("data len: {:?}", data.len());
    }
}

// use std::ffi::CString;
// use std::io::{BufRead, Error, ErrorKind, Read, Result, Write};
// use std::time;
use alloc::vec::Vec;
use bytes::Buf;
use miniz_oxide::inflate::decompress_to_vec;

const FHCRC: u8 = 1 << 1;
const FEXTRA: u8 = 1 << 2;
const FNAME: u8 = 1 << 3;
const FCOMMENT: u8 = 1 << 4;
const FRESERVED: u8 = 1 << 5 | 1 << 6 | 1 << 7;

pub fn decompress_gz(buffer: &[u8]) -> Result<Vec<u8>, &'static str> {
    let [m0, m1, cm, flags, ..] = *buffer else {
        return Err("truncated header");
    };
    // let header : &[u8; 4] = buffer.get(0..4).ok_or("truncated header")?.try_into().map_err(|_| "err")?;
    if m0 != 0x1f || m1 != 0x8b {
        return Err("invalid magic number");
    }
    if cm != 8 {
        return Err("invalid compression method");
    }
    if flags & FRESERVED != 0 {
        return Err("invalid flags");
    }
    if flags & FEXTRA != 0 {
        return Err("extra data not supported");
    }
    if flags & FNAME != 0 {
        return Err("filename not supported");
    }
    if flags & FCOMMENT != 0 {
        return Err("comment not supported");
    }
    if flags & FHCRC != 0 {
        return Err("crc not supported");
    }
    let trailer_start = buffer.len() - 8;
    let crc_bytes = buffer.get(trailer_start..trailer_start + 4).ok_or("truncated crc")?;
    let crc = u32::from_le_bytes(crc_bytes.try_into().map_err(|_| "err")?);
    let isize = u32::from_le_bytes(
        buffer.get(trailer_start + 4..trailer_start + 8).ok_or("err")?
            .try_into()
            .map_err(|_| "err")?,
    );
    let compressed_bytes = buffer.get(10..trailer_start).ok_or("truncated data")?;
    let data = decompress_to_vec(compressed_bytes).map_err(|_| "failed to decompress")?;
    if isize != data.len() as u32 {
        return Err("invalid isize");
    }
    let crc_calc = crc32fast::hash(&data);
    if crc_calc != crc {
        return Err("crc mismatch");
    }
    Ok(data)
}

#[cfg(test)]
mod tests {
    extern crate std;
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

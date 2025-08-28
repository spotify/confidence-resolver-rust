// use std::ffi::CString;
// use std::io::{BufRead, Error, ErrorKind, Read, Result, Write};
// use std::time;
use alloc::vec::Vec;
use miniz_oxide::inflate::decompress_to_vec;

const FHCRC: u8 = 1 << 1;
const FEXTRA: u8 = 1 << 2;
const FNAME: u8 = 1 << 3;
const FCOMMENT: u8 = 1 << 4;
const FRESERVED: u8 = 1 << 5 | 1 << 6 | 1 << 7;

pub fn decompress_gz(buffer: &[u8]) -> Result<Vec<u8>, &'static str> {
    if buffer[0] != 0x1f || buffer[1] != 0x8b {
        return Err("invalid magic number");
    }
    if buffer[2] != 8 {
        return Err("invalid compression method");
    }
    let flags = buffer[3];
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
    let crc = u32::from_le_bytes(buffer[trailer_start..trailer_start + 4].try_into().unwrap());
    let isize = u32::from_le_bytes(
        buffer[trailer_start + 4..trailer_start + 8]
            .try_into()
            .unwrap(),
    );
    let data = decompress_to_vec(&buffer[10..trailer_start]).map_err(|_| "failed to decompress")?;
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

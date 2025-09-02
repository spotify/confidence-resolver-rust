use crate::confidence::flags::admin::v1::context_field_semantic_type::country_semantic_type::CountryFormat;
use crate::confidence::flags::admin::v1::{
    context_field_semantic_type, evaluation_context_schema_field, ContextFieldSemanticType,
};
use alloc::format;
use alloc::string::{String, ToString};
use alloc::vec::Vec;
use chrono::{DateTime, NaiveDate, NaiveDateTime, Utc};

#[cfg(feature = "std")]
use pbjson_types::{value::Kind, Struct, Value};
#[cfg(not(feature = "std"))]
pub use prost_types::{value::Kind, Struct, Value};

use crate::confidence::flags::admin::v1::context_field_semantic_type::{
    CountrySemanticType, DateSemanticType, TimestampSemanticType, VersionSemanticType,
};
use alloc::collections::{BTreeMap, BTreeSet};
use isocountry::CountryCode;

#[derive(Debug, Clone)]
pub struct DerivedClientSchema {
    pub fields: BTreeMap<String, evaluation_context_schema_field::Kind>,
    pub semantic_types: BTreeMap<String, ContextFieldSemanticType>,
}

pub struct SchemaFromEvaluationContext;

impl SchemaFromEvaluationContext {
    const MIN_DATE_LENGTH: usize = "2025-04-01".len();
    const MIN_TIMESTAMP_LENGTH: usize = "2025-04-01T0000".len();

    pub fn get_schema(evaluation_context: &Struct) -> DerivedClientSchema {
        let mut flat_schema = BTreeMap::new();
        let mut semantic_types = BTreeMap::new();

        Self::flattened_schema(
            evaluation_context,
            "",
            &mut flat_schema,
            &mut semantic_types,
        );

        DerivedClientSchema {
            fields: flat_schema,
            semantic_types,
        }
    }

    fn flattened_schema(
        struct_value: &Struct,
        field_path: &str,
        flat_schema: &mut BTreeMap<String, evaluation_context_schema_field::Kind>,
        semantic_types: &mut BTreeMap<String, ContextFieldSemanticType>,
    ) {
        for (field, value) in &struct_value.fields {
            if let Some(Kind::StructValue(nested_struct)) = &value.kind {
                Self::flattened_schema(
                    nested_struct,
                    &format!("{}{}.", field_path, field),
                    flat_schema,
                    semantic_types,
                );
            } else {
                Self::add_field_schema(
                    value,
                    &format!("{}{}", field_path, field),
                    flat_schema,
                    semantic_types,
                );
            }
        }
    }

    fn add_field_schema(
        value: &Value,
        field_path: &str,
        flat_schema: &mut BTreeMap<String, evaluation_context_schema_field::Kind>,
        semantic_types: &mut BTreeMap<String, ContextFieldSemanticType>,
    ) {
        match &value.kind {
            Some(Kind::StringValue(string_val)) => {
                flat_schema.insert(
                    field_path.to_string(),
                    evaluation_context_schema_field::Kind::StringKind,
                );
                Self::guess_semantic_type(string_val, field_path, semantic_types);
            }
            Some(Kind::BoolValue(_)) => {
                flat_schema.insert(
                    field_path.to_string(),
                    evaluation_context_schema_field::Kind::BoolKind,
                );
            }
            Some(Kind::NumberValue(_)) => {
                flat_schema.insert(
                    field_path.to_string(),
                    evaluation_context_schema_field::Kind::NumberKind,
                );
            }
            Some(Kind::NullValue(_)) => {
                flat_schema.insert(
                    field_path.to_string(),
                    evaluation_context_schema_field::Kind::NullKind,
                );
            }
            Some(Kind::ListValue(list_val)) => {
                if !list_val.values.is_empty() {
                    let first_kind = list_val.values.iter().find_map(|v| v.kind.as_ref());

                    if let Some(first_kind) = first_kind {
                        let all_same_kind = list_val
                            .values
                            .iter()
                            .filter_map(|v| v.kind.as_ref())
                            .all(|kind| {
                                core::mem::discriminant(kind) == core::mem::discriminant(first_kind)
                            });

                        if all_same_kind {
                            Self::add_field_schema(
                                &list_val.values[0],
                                field_path,
                                flat_schema,
                                semantic_types,
                            );
                        }
                    }
                }
            }
            _ => {}
        }
    }

    fn guess_semantic_type(
        value: &str,
        field_path: &str,
        semantic_types: &mut BTreeMap<String, ContextFieldSemanticType>,
    ) {
        let lower_case_path = field_path.to_lowercase();

        if lower_case_path.contains("country") {
            if Self::is_valid_country_code(value) {
                semantic_types.insert(
                    field_path.to_string(),
                    ContextFieldSemanticType {
                        r#type: Some(context_field_semantic_type::Type::Country(
                            CountrySemanticType {
                                format: CountryFormat::TwoLetterIsoCode.into(),
                            },
                        )),
                    },
                );
            }
        } else if Self::is_date(value) {
            semantic_types.insert(
                field_path.to_string(),
                ContextFieldSemanticType {
                    r#type: Some(context_field_semantic_type::Type::Date(
                        DateSemanticType::default(),
                    )),
                },
            );
        } else if Self::is_timestamp(value) {
            semantic_types.insert(
                field_path.to_string(),
                ContextFieldSemanticType {
                    r#type: Some(context_field_semantic_type::Type::Timestamp(
                        TimestampSemanticType::default(),
                    )),
                },
            );
        } else if Self::is_semantic_version(value) {
            semantic_types.insert(
                field_path.to_string(),
                ContextFieldSemanticType {
                    r#type: Some(context_field_semantic_type::Type::Version(
                        VersionSemanticType::default(),
                    )),
                },
            );
        }
    }

    fn is_semantic_version(value: &str) -> bool {
        // Implement semantic version validation
        // This is a simplified version - you might want to use a proper semver crate
        let parts: Vec<&str> = value.split('.').collect();
        if parts.len() != 3 {
            return false;
        }

        parts.iter().all(|part| part.parse::<u32>().is_ok())
    }

    fn is_timestamp(value: &str) -> bool {
        if value.len() < Self::MIN_TIMESTAMP_LENGTH {
            return false;
        }
        Self::parse_instant(value).is_some()
    }

    fn is_date(value: &str) -> bool {
        if value.len() < Self::MIN_DATE_LENGTH {
            return false;
        }

        NaiveDate::parse_from_str(value, "%Y-%m-%d").is_ok()
    }

    fn is_valid_country_code(value: &str) -> bool {
        // ISO 3166-1 alpha-2 country codes
        let country_codes = get_iso_country_codes();
        country_codes.contains(&value.to_uppercase().as_str())
    }

    fn parse_instant(value: &str) -> Option<DateTime<Utc>> {
        if value.is_empty() {
            return None;
        }

        // Try parsing as RFC3339/ISO8601 with timezone
        if let Ok(dt) = DateTime::parse_from_rfc3339(value) {
            return Some(dt.with_timezone(&Utc));
        }

        // Try parsing with custom formats
        if value.contains('T') {
            let t_pos = value.find('T').unwrap();
            let time_part = &value[t_pos..];

            if value.ends_with('Z') || time_part.contains('+') || time_part.contains('-') {
                // Try parsing as zoned datetime
                if let Ok(dt) = DateTime::parse_from_rfc3339(value) {
                    return Some(dt.with_timezone(&Utc));
                }
            } else {
                // Try parsing as local datetime and assume UTC
                if let Ok(ndt) = NaiveDateTime::parse_from_str(value, "%Y-%m-%dT%H:%M:%S") {
                    return Some(DateTime::from_naive_utc_and_offset(ndt, Utc));
                }
                if let Ok(ndt) = NaiveDateTime::parse_from_str(value, "%Y-%m-%dT%H:%M:%S%.f") {
                    return Some(DateTime::from_naive_utc_and_offset(ndt, Utc));
                }
            }
        } else {
            // Try parsing as date only
            if let Ok(nd) = NaiveDate::parse_from_str(value, "%Y-%m-%d") {
                let ndt = nd.and_hms_opt(0, 0, 0).unwrap();
                return Some(DateTime::from_naive_utc_and_offset(ndt, Utc));
            }
        }

        None
    }
}

fn get_iso_country_codes() -> BTreeSet<&'static str> {
    CountryCode::iter().map(|cc| cc.alpha2()).collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[cfg(feature = "std")]
    use std::collections::HashMap;

    // Match Struct.fields backing map depending on feature:
    // - With "std" feature, pbjson_types::Struct uses HashMap
    // - Without "std", prost_types::Struct uses BTreeMap
    #[cfg(feature = "std")]
    type MapType<K, V> = HashMap<K, V>;
    #[cfg(not(feature = "std"))]
    type MapType<K, V> = BTreeMap<K, V>;

    // Helper function to create a Value with a string
    fn string_value(s: &str) -> Value {
        Value {
            kind: Some(Kind::StringValue(s.to_string())),
        }
    }

    // Helper function to create a Value with a number
    fn number_value(n: f64) -> Value {
        Value {
            kind: Some(Kind::NumberValue(n)),
        }
    }

    // Helper function to create a Value with a boolean
    fn bool_value(b: bool) -> Value {
        Value {
            kind: Some(Kind::BoolValue(b)),
        }
    }

    // Helper function to create a Value with null
    fn null_value() -> Value {
        Value {
            kind: Some(Kind::NullValue(0)), // prost uses i32 for null
        }
    }

    // Helper function to create a nested struct
    fn struct_value(fields: MapType<String, Value>) -> Value {
        Value {
            kind: Some(Kind::StructValue(Struct { fields })),
        }
    }

    #[test]
    fn test_flat_schema_basic_types() {
        let mut fields = MapType::new();
        fields.insert("name".to_string(), string_value("John"));
        fields.insert("age".to_string(), number_value(30.0));
        fields.insert("active".to_string(), bool_value(true));
        fields.insert("metadata".to_string(), null_value());

        let evaluation_context = Struct { fields };
        let schema = SchemaFromEvaluationContext::get_schema(&evaluation_context);

        // Check schema field kinds
        assert_eq!(
            schema.fields.get("name"),
            Some(&evaluation_context_schema_field::Kind::StringKind)
        );
        assert_eq!(
            schema.fields.get("age"),
            Some(&evaluation_context_schema_field::Kind::NumberKind)
        );
        assert_eq!(
            schema.fields.get("active"),
            Some(&evaluation_context_schema_field::Kind::BoolKind)
        );
        assert_eq!(
            schema.fields.get("metadata"),
            Some(&evaluation_context_schema_field::Kind::NullKind)
        );

        // No semantic types for basic types without special patterns
        assert!(schema.semantic_types.is_empty());
    }

    #[test]
    fn test_nested_schema_flattening() {
        let mut profile_fields = MapType::new();
        profile_fields.insert("country".to_string(), string_value("US"));
        profile_fields.insert("age".to_string(), number_value(25.0));

        let mut address_fields = MapType::new();
        address_fields.insert("city".to_string(), string_value("New York"));
        address_fields.insert("zip".to_string(), string_value("10001"));

        profile_fields.insert("address".to_string(), struct_value(address_fields));

        let mut user_fields = MapType::new();
        user_fields.insert("id".to_string(), string_value("user123"));
        user_fields.insert("profile".to_string(), struct_value(profile_fields));

        let mut fields = MapType::new();
        fields.insert("user".to_string(), struct_value(user_fields));

        let evaluation_context = Struct { fields };
        let schema = SchemaFromEvaluationContext::get_schema(&evaluation_context);

        // Check flattened field names
        assert!(schema.fields.contains_key("user.id"));
        assert!(schema.fields.contains_key("user.profile.country"));
        assert!(schema.fields.contains_key("user.profile.age"));
        assert!(schema.fields.contains_key("user.profile.address.city"));
        assert!(schema.fields.contains_key("user.profile.address.zip"));

        // Check field types
        assert_eq!(
            schema.fields.get("user.id"),
            Some(&evaluation_context_schema_field::Kind::StringKind)
        );
        assert_eq!(
            schema.fields.get("user.profile.age"),
            Some(&evaluation_context_schema_field::Kind::NumberKind)
        );

        // Check semantic type for country field
        assert!(schema.semantic_types.contains_key("user.profile.country"));
        let country_semantic_type = schema.semantic_types.get("user.profile.country").unwrap();
        assert!(matches!(
            country_semantic_type.r#type,
            Some(context_field_semantic_type::Type::Country(_))
        ));
    }

    #[test]
    fn test_country_semantic_type_detection() {
        let mut fields = MapType::new();
        fields.insert("user_country".to_string(), string_value("US"));
        fields.insert("shipping_country".to_string(), string_value("CA"));
        fields.insert("invalid_country".to_string(), string_value("XX"));
        fields.insert("location_code".to_string(), string_value("US")); // US but no "country" in field name

        let evaluation_context = Struct { fields };
        let schema = SchemaFromEvaluationContext::get_schema(&evaluation_context);

        // Valid country codes with "country" in field name should be detected
        assert!(schema.semantic_types.contains_key("user_country"));
        assert!(schema.semantic_types.contains_key("shipping_country"));

        // Invalid country code should not be detected
        assert!(!schema.semantic_types.contains_key("invalid_country"));

        // Valid country code but no "country" in field name should not be detected
        assert!(!schema.semantic_types.contains_key("location_code"));

        // Check the semantic type structure
        let country_semantic_type = schema.semantic_types.get("user_country").unwrap();
        if let Some(context_field_semantic_type::Type::Country(country_type)) =
            &country_semantic_type.r#type
        {
            assert_eq!(country_type.format, CountryFormat::TwoLetterIsoCode as i32);
        } else {
            panic!("Expected country semantic type");
        }
    }

    #[test]
    fn test_date_semantic_type_detection() {
        let mut fields = MapType::new();
        fields.insert("birth_date".to_string(), string_value("2023-05-15"));
        fields.insert("created_at".to_string(), string_value("2023-12-01"));
        fields.insert("invalid_date".to_string(), string_value("not-a-date"));
        fields.insert("partial_date".to_string(), string_value("2023-05"));

        let evaluation_context = Struct { fields };
        let schema = SchemaFromEvaluationContext::get_schema(&evaluation_context);

        // Valid dates should be detected
        assert!(schema.semantic_types.contains_key("birth_date"));
        assert!(schema.semantic_types.contains_key("created_at"));

        // Invalid dates should not be detected
        assert!(!schema.semantic_types.contains_key("invalid_date"));
        assert!(!schema.semantic_types.contains_key("partial_date"));

        // Check the semantic type structure
        let date_semantic_type = schema.semantic_types.get("birth_date").unwrap();
        assert!(matches!(
            date_semantic_type.r#type,
            Some(context_field_semantic_type::Type::Date(_))
        ));
    }

    #[test]
    fn test_timestamp_semantic_type_detection() {
        let mut fields = MapType::new();
        fields.insert(
            "created_at".to_string(),
            string_value("2023-05-15T10:30:00Z"),
        );
        fields.insert(
            "updated_at".to_string(),
            string_value("2023-05-15T10:30:00"),
        );
        fields.insert(
            "event_time".to_string(),
            string_value("2023-05-15T10:30:00.123Z"),
        );
        fields.insert(
            "invalid_timestamp".to_string(),
            string_value("not-a-timestamp"),
        );
        fields.insert("short_string".to_string(), string_value("short"));

        let evaluation_context = Struct { fields };
        let schema = SchemaFromEvaluationContext::get_schema(&evaluation_context);

        // Valid timestamps should be detected
        assert!(schema.semantic_types.contains_key("created_at"));
        assert!(schema.semantic_types.contains_key("updated_at"));
        assert!(schema.semantic_types.contains_key("event_time"));

        // Invalid timestamps should not be detected
        assert!(!schema.semantic_types.contains_key("invalid_timestamp"));
        assert!(!schema.semantic_types.contains_key("short_string"));

        // Check the semantic type structure
        let timestamp_semantic_type = schema.semantic_types.get("created_at").unwrap();
        assert!(matches!(
            timestamp_semantic_type.r#type,
            Some(context_field_semantic_type::Type::Timestamp(_))
        ));
    }

    #[test]
    fn test_version_semantic_type_detection() {
        let mut fields = MapType::new();
        fields.insert("app_version".to_string(), string_value("1.2.3"));
        fields.insert("api_version".to_string(), string_value("10.0.1"));
        fields.insert("lib_version".to_string(), string_value("0.1.0"));
        fields.insert("invalid_version".to_string(), string_value("1.2"));
        fields.insert("bad_version".to_string(), string_value("1.2.3.4"));
        fields.insert("non_numeric_version".to_string(), string_value("v1.2.3"));

        let evaluation_context = Struct { fields };
        let schema = SchemaFromEvaluationContext::get_schema(&evaluation_context);

        // Valid semantic versions should be detected
        assert!(schema.semantic_types.contains_key("app_version"));
        assert!(schema.semantic_types.contains_key("api_version"));
        assert!(schema.semantic_types.contains_key("lib_version"));

        // Invalid versions should not be detected
        assert!(!schema.semantic_types.contains_key("invalid_version"));
        assert!(!schema.semantic_types.contains_key("bad_version"));
        assert!(!schema.semantic_types.contains_key("non_numeric_version"));

        // Check the semantic type structure
        let version_semantic_type = schema.semantic_types.get("app_version").unwrap();
        assert!(matches!(
            version_semantic_type.r#type,
            Some(context_field_semantic_type::Type::Version(_))
        ));
    }

    #[test]
    fn test_semantic_type_priority() {
        // Test that timestamps take priority over dates when both could match
        let mut fields = MapType::new();
        fields.insert(
            "timestamp_field".to_string(),
            string_value("2023-05-15T10:30:00Z"),
        );

        let evaluation_context = Struct { fields };
        let schema = SchemaFromEvaluationContext::get_schema(&evaluation_context);

        let semantic_type = schema.semantic_types.get("timestamp_field").unwrap();

        // Should be detected as timestamp, not date
        assert!(matches!(
            semantic_type.r#type,
            Some(context_field_semantic_type::Type::Timestamp(_))
        ));
    }

    #[test]
    fn test_complex_nested_evaluation_context() {
        // Create a complex nested structure
        let mut device_fields = MapType::new();
        device_fields.insert("type".to_string(), string_value("mobile"));
        device_fields.insert("os_version".to_string(), string_value("15.2.1"));

        let mut location_fields = MapType::new();
        location_fields.insert("country".to_string(), string_value("FR"));
        location_fields.insert("city".to_string(), string_value("Paris"));

        let mut user_fields = MapType::new();
        user_fields.insert("id".to_string(), string_value("user_12345"));
        user_fields.insert(
            "created_at".to_string(),
            string_value("2023-01-15T09:30:00Z"),
        );
        user_fields.insert("last_login".to_string(), string_value("2023-12-01"));
        user_fields.insert("is_premium".to_string(), bool_value(true));
        user_fields.insert("device".to_string(), struct_value(device_fields));
        user_fields.insert("location".to_string(), struct_value(location_fields));

        let mut fields = MapType::new();
        fields.insert("user".to_string(), struct_value(user_fields));
        fields.insert("app_version".to_string(), string_value("2.1.0"));

        let evaluation_context = Struct { fields };
        let schema = SchemaFromEvaluationContext::get_schema(&evaluation_context);

        // Check all flattened fields exist
        assert!(schema.fields.contains_key("user.id"));
        assert!(schema.fields.contains_key("user.created_at"));
        assert!(schema.fields.contains_key("user.last_login"));
        assert!(schema.fields.contains_key("user.is_premium"));
        assert!(schema.fields.contains_key("user.device.type"));
        assert!(schema.fields.contains_key("user.device.os_version"));
        assert!(schema.fields.contains_key("user.location.country"));
        assert!(schema.fields.contains_key("user.location.city"));
        assert!(schema.fields.contains_key("app_version"));

        // Check field types
        assert_eq!(
            schema.fields.get("user.is_premium"),
            Some(&evaluation_context_schema_field::Kind::BoolKind)
        );
        assert_eq!(
            schema.fields.get("user.device.type"),
            Some(&evaluation_context_schema_field::Kind::StringKind)
        );

        // Check semantic types
        assert!(schema.semantic_types.contains_key("user.created_at")); // timestamp
        assert!(schema.semantic_types.contains_key("user.last_login")); // date
        assert!(schema.semantic_types.contains_key("user.device.os_version")); // version
        assert!(schema.semantic_types.contains_key("user.location.country")); // country
        assert!(schema.semantic_types.contains_key("app_version")); // version

        // Verify specific semantic types
        let timestamp_type = schema.semantic_types.get("user.created_at").unwrap();
        assert!(matches!(
            timestamp_type.r#type,
            Some(context_field_semantic_type::Type::Timestamp(_))
        ));

        let date_type = schema.semantic_types.get("user.last_login").unwrap();
        assert!(matches!(
            date_type.r#type,
            Some(context_field_semantic_type::Type::Date(_))
        ));

        let country_type = schema.semantic_types.get("user.location.country").unwrap();
        assert!(matches!(
            country_type.r#type,
            Some(context_field_semantic_type::Type::Country(_))
        ));
    }
}

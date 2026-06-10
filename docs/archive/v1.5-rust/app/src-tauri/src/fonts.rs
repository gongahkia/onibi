fn normalize_font_families(mut names: Vec<String>) -> Vec<String> {
    names.retain(|name| !name.trim().is_empty());
    for name in &mut names {
        *name = name.trim().to_string();
    }
    names.sort_by(|left, right| {
        left.to_lowercase()
            .cmp(&right.to_lowercase())
            .then_with(|| left.cmp(right))
    });
    names.dedup_by(|left, right| left.eq_ignore_ascii_case(right));
    names
}

#[tauri::command]
pub async fn list_font_families() -> Vec<String> {
    let mut database = fontdb::Database::new();
    database.load_system_fonts();
    let names = database
        .faces()
        .flat_map(|face| face.families.iter().map(|(family, _)| family.to_string()))
        .collect();
    normalize_font_families(names)
}

#[cfg(test)]
mod tests {
    use super::normalize_font_families;

    #[test]
    fn normalizes_sorting_and_duplicates() {
        let families = normalize_font_families(vec![
            " Zed ".to_string(),
            "alpha".to_string(),
            "Alpha".to_string(),
            "".to_string(),
            "Beta".to_string(),
        ]);

        assert_eq!(families, vec!["Alpha", "Beta", "Zed"]);
    }
}

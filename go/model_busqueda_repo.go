package openapi

import (
    "database/sql"
    "fmt"
    "strconv"
    "strings"
)

// ResolveGeneroID returns the genre id for a given parameter which can be an ID or a name.
func ResolveGeneroID(db *sql.DB, generoParam string) (*int, error) {
    generoParam = strings.TrimSpace(generoParam)
    if generoParam == "" {
        return nil, nil
    }
    if id, err := strconv.Atoi(generoParam); err == nil {
        return &id, nil
    }
    var gid int
    // Try exact (case-insensitive) match first
    err := db.QueryRow("SELECT id FROM genero WHERE lower(nombre)=lower($1) LIMIT 1", generoParam).Scan(&gid)
    if err == nil {
        return &gid, nil
    }
    // Fallback to partial match using ILIKE
    like := "%" + generoParam + "%"
    if err2 := db.QueryRow("SELECT id FROM genero WHERE nombre ILIKE $1 LIMIT 1", like).Scan(&gid); err2 == nil {
        return &gid, nil
    }
    return nil, nil // not found is not an error for our use case
}

// SearchAlbums queries albums with optional name and genre filter, returns items and total count.
func SearchAlbums(db *sql.DB, q string, generoID *int, page, perPage int) ([]AlbumResult, int, error) {
    likeQ := "%"
    if q != "" {
        likeQ = "%" + q + "%"
    }
    var where []string
    var args []interface{}
    idx := 1
    if q != "" {
        where = append(where, fmt.Sprintf("nombre ILIKE $%d", idx))
        args = append(args, likeQ)
        idx++
    }
    if generoID != nil {
        where = append(where, fmt.Sprintf("genero = $%d", idx))
        args = append(args, *generoID)
        idx++
    }
    whereClause := ""
    if len(where) > 0 {
        whereClause = "WHERE " + strings.Join(where, " AND ")
    }
    // count
    var total int
    if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM album %s", whereClause), args...).Scan(&total); err != nil {
        return nil, 0, err
    }
    // page
    offset := (page - 1) * perPage
    selQuery := fmt.Sprintf("SELECT id,nombre,duracion,urlimagen,fecha,genero,artista FROM album %s ORDER BY id LIMIT $%d OFFSET $%d", whereClause, len(args)+1, len(args)+2)
    args = append(args, perPage, offset)
    rows, err := db.Query(selQuery, args...)
    if err != nil {
        return nil, total, err
    }
    defer rows.Close()
    var items []AlbumResult
    for rows.Next() {
        var a AlbumResult
        var fecha sql.NullString
        if err := rows.Scan(&a.Id, &a.Nombre, &a.Duracion, &a.UrlImagen, &fecha, &a.Genero, &a.Artista); err != nil {
            return nil, total, err
        }
        if fecha.Valid {
            a.Fecha = fecha.String
        }
        items = append(items, a)
    }
    return items, total, nil
}

// SearchCanciones queries songs with optional name and genre filter.
func SearchCanciones(db *sql.DB, q string, generoID *int, page, perPage int) ([]CancionResult, int, error) {
    likeQ := "%"
    if q != "" {
        likeQ = "%" + q + "%"
    }
    var where []string
    var args []interface{}
    idx := 1
    if q != "" {
        where = append(where, fmt.Sprintf("nombre ILIKE $%d", idx))
        args = append(args, likeQ)
        idx++
    }
    if generoID != nil {
        where = append(where, fmt.Sprintf("album IN (SELECT id FROM album WHERE genero = $%d)", idx))
        args = append(args, *generoID)
        idx++
    }
    whereClause := ""
    if len(where) > 0 {
        whereClause = "WHERE " + strings.Join(where, " AND ")
    }
    var total int
    if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM cancion %s", whereClause), args...).Scan(&total); err != nil {
        return nil, 0, err
    }
    offset := (page - 1) * perPage
    selQuery := fmt.Sprintf("SELECT id,nombre,urlimagen,duracion,album FROM cancion %s ORDER BY id LIMIT $%d OFFSET $%d", whereClause, len(args)+1, len(args)+2)
    args = append(args, perPage, offset)
    rows, err := db.Query(selQuery, args...)
    if err != nil {
        return nil, total, err
    }
    defer rows.Close()
    var items []CancionResult
    for rows.Next() {
        var s CancionResult
        if err := rows.Scan(&s.Id, &s.Nombre, &s.UrlImagen, &s.Duracion, &s.Album); err != nil {
            return nil, total, err
        }
        items = append(items, s)
    }
    return items, total, nil
}

// SearchMerch queries merch products with optional name filter.
func SearchMerch(db *sql.DB, q string, page, perPage int) ([]Merch, int, error) {
    likeQ := "%"
    if q != "" {
        likeQ = "%" + q + "%"
    }
    var where []string
    var args []interface{}
    idx := 1
    if q != "" {
        where = append(where, fmt.Sprintf("nombre ILIKE $%d", idx))
        args = append(args, likeQ)
        idx++
    }
    whereClause := ""
    if len(where) > 0 {
        whereClause = "WHERE " + strings.Join(where, " AND ")
    }
    var total int
    if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM merchandising %s", whereClause), args...).Scan(&total); err != nil {
        return nil, 0, err
    }
    offset := (page - 1) * perPage
    selQuery := fmt.Sprintf("SELECT id,nombre,precio,imagen,artista,stock FROM merchandising %s ORDER BY id LIMIT $%d OFFSET $%d", whereClause, len(args)+1, len(args)+2)
    args = append(args, perPage, offset)
    rows, err := db.Query(selQuery, args...)
    if err != nil {
        return nil, total, err
    }
    defer rows.Close()
    var items []Merch
    for rows.Next() {
        var m Merch
        if err := rows.Scan(&m.Id, &m.Nombre, &m.Precio, &m.Imagen, &m.Artista, &m.Stock); err != nil {
            return nil, total, err
        }
        items = append(items, m)
    }
    return items, total, nil
}

// SearchArtistas collects artists IDs by presence in album, cancion and merchandising; returns aggregated counts.
func SearchArtistas(db *sql.DB, q string, generoID *int) ([]ArtistResult, int, error) {
    likeQ := "%"
    if q != "" {
        likeQ = "%" + q + "%"
    }
    artistsMap := map[int32]*ArtistResult{}

    // from albums
    {
        var args []interface{}
        var where []string
        idx := 1
        if q != "" {
            where = append(where, fmt.Sprintf("nombre ILIKE $%d", idx))
            args = append(args, likeQ)
            idx++
        }
        if generoID != nil {
            where = append(where, fmt.Sprintf("genero = $%d", idx))
            args = append(args, *generoID)
            idx++
        }
        whereClause := ""
        if len(where) > 0 {
            whereClause = "WHERE " + strings.Join(where, " AND ")
        }
        rows, err := db.Query(fmt.Sprintf("SELECT DISTINCT artista FROM album %s", whereClause), args...)
        if err == nil {
            defer rows.Close()
            for rows.Next() {
                var id sql.NullInt64
                if err := rows.Scan(&id); err == nil && id.Valid {
                    aid := int32(id.Int64)
                    ar := artistsMap[aid]
                    if ar == nil {
                        ar = &ArtistResult{Id: aid}
                        artistsMap[aid] = ar
                    }
                    ar.AlbumsCount++
                }
            }
        }
    }

    // from artista_cancion join cancion
    {
        var args []interface{}
        var where []string
        idx := 1
        if q != "" {
            where = append(where, fmt.Sprintf("c.nombre ILIKE $%d", idx))
            args = append(args, likeQ)
            idx++
        }
        whereClause := ""
        if len(where) > 0 {
            whereClause = "WHERE " + strings.Join(where, " AND ")
        }
        query := fmt.Sprintf("SELECT DISTINCT ac.artista FROM artista_cancion ac JOIN cancion c ON ac.cancion = c.id %s", whereClause)
        rows, err := db.Query(query, args...)
        if err == nil {
            defer rows.Close()
            for rows.Next() {
                var id sql.NullInt64
                if err := rows.Scan(&id); err == nil && id.Valid {
                    aid := int32(id.Int64)
                    ar := artistsMap[aid]
                    if ar == nil {
                        ar = &ArtistResult{Id: aid}
                        artistsMap[aid] = ar
                    }
                    ar.SongsCount++
                }
            }
        }
    }

    // from merchandising
    {
        var args []interface{}
        var where []string
        idx := 1
        if q != "" {
            where = append(where, fmt.Sprintf("nombre ILIKE $%d", idx))
            args = append(args, likeQ)
            idx++
        }
        whereClause := ""
        if len(where) > 0 {
            whereClause = "WHERE " + strings.Join(where, " AND ")
        }
        rows, err := db.Query(fmt.Sprintf("SELECT DISTINCT artista FROM merchandising %s", whereClause), args...)
        if err == nil {
            defer rows.Close()
            for rows.Next() {
                var id sql.NullInt64
                if err := rows.Scan(&id); err == nil && id.Valid {
                    aid := int32(id.Int64)
                    ar := artistsMap[aid]
                    if ar == nil {
                        ar = &ArtistResult{Id: aid}
                        artistsMap[aid] = ar
                    }
                    ar.MerchCount++
                }
            }
        }
    }

    var artists []ArtistResult
    for _, a := range artistsMap {
        artists = append(artists, *a)
    }
    return artists, len(artists), nil
}

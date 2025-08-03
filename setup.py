#!/usr/bin/env python3

import sqlite3
import os
from PIL import Image
from dataclasses import dataclass
from typing import Optional, Tuple


@dataclass
class ImageData:
    name: str
    path: str
    http: str
    idx: int
    orientation: str
    width: int
    height: int


def img_orient(img_path: str) -> Tuple[int, int, str]:
    """
    Determine the orientation of an image based on its dimensions.
    Returns 'landscape', 'portrait', or 'square'.
    """
    try:
        with Image.open(img_path) as img:
            width, height = img.size
            
            if width > height:
                print("Landscape")
                return width, height, "landscape"
            elif width < height:
                print("Portrait")
                return width, height, "portrait"
            else:
                print("Square")
                return width, height, "square"
    except Exception as e:
        raise Exception(f"Error processing image {img_path}: {e}")


def create_img_db_table(db_path: str) -> None:
    """
    Create the images table in the SQLite database if it doesn't exist.
    """
    try:
        conn = sqlite3.connect(db_path)
        cursor = conn.cursor()
        
        create_table_sql = """
        CREATE TABLE IF NOT EXISTS images (
            Name TEXT,
            Path TEXT,
            Http TEXT,
            Idx INTEGER,
            Orientation TEXT,
            Width INTEGER,
            Height INTEGER
        );"""
        
        cursor.execute(create_table_sql)
        conn.commit()
        conn.close()
        
    except sqlite3.Error as e:
        print(f"Failed to create table: {e}")


def create_http_path(fpath: str) -> str:
    """
    Convert file system path to HTTP path by replacing the base directory.
    """
    return fpath.replace("/media/whitepi/ATree/", "/static/")


def walk_img_dir(db_path: str, directory: str) -> Optional[Exception]:
    """
    Walk through the directory, find JPEG images, and insert their data into the database.
    """
    idx = 0
    failed_images = []
    
    try:
        conn = sqlite3.connect(db_path)
        cursor = conn.cursor()
        
        for root, dirs, files in os.walk(directory):
            for file in files:
                idx += 1
                file_path = os.path.join(root, file)
                ext = os.path.splitext(file.lower())[1]
                
                if ext == ".jpg":
                    try:
                        width, height, orientation = img_orient(file_path)
                        
                        image_data = ImageData(
                            name=file,
                            path=file_path,
                            http=create_http_path(file_path),
                            idx=idx,
                            orientation=orientation,
                            width=width,
                            height=height
                        )
                        
                        print(image_data)
                        
                        insert_sql = """INSERT INTO images (Name, Path, Http, Idx, Orientation, Width, Height) 
                                       VALUES (?, ?, ?, ?, ?, ?, ?)"""
                        cursor.execute(insert_sql, (
                            image_data.name,
                            image_data.path,
                            image_data.http,
                            image_data.idx,
                            image_data.orientation,
                            image_data.width,
                            image_data.height,
                        ))
                        
                    except Exception as e:
                        print(f"Skipping image {file_path}: {e}")
                        failed_images.append(file_path)
                        continue
        
        conn.commit()
        conn.close()
        
        # Print summary of failed images
        if failed_images:
            print(f"\n--- Summary ---")
            print(f"Failed to process {len(failed_images)} image(s):")
            for failed_img in failed_images:
                print(f"  - {failed_img}")
        else:
            print(f"\n--- Summary ---")
            print("All images processed successfully!")
        
        return None
        
    except sqlite3.Error as e:
        print(f"Database error: {e}")
        return e


def main():
    """
    Main function to set up the database and process images.
    """
    db_path = "/home/whitepi/go/slideshowgodocker/imagesDB"
    image_dir = "/media/whitepi/ATree/test2/"
    
    create_img_db_table(db_path)
    walk_img_dir(db_path, image_dir)


if __name__ == "__main__":
    main()

#!/usr/bin/env python3

import os
import shutil
from PIL import Image, ExifTags
from pathlib import Path


def ensure_directory_exists(directory_path: str) -> None:
    """
    Create directory if it doesn't exist.
    """
    Path(directory_path).mkdir(parents=True, exist_ok=True)
    print(f"Directory ensured: {directory_path}")


def calculate_new_dimensions(width: int, height: int, max_size: int = 1500) -> tuple[int, int]:
    """
    Calculate new dimensions maintaining aspect ratio.
    If either dimension > max_size, resize keeping aspect ratio.
    """
    if width <= max_size and height <= max_size:
        return width, height
    
    # Calculate aspect ratio
    aspect_ratio = width / height
    
    if width > height:
        # Width is the limiting factor
        new_width = max_size
        new_height = int(max_size / aspect_ratio)
    else:
        # Height is the limiting factor
        new_height = max_size
        new_width = int(max_size * aspect_ratio)
    
    return new_width, new_height


def get_orientation_tag_id():
    """
    Get the EXIF tag ID for orientation in a compatible way.
    """
    # Look for the orientation tag ID
    for tag_id, tag_name in ExifTags.TAGS.items():
        if tag_name == 'Orientation':
            return tag_id
    return 274  # Standard EXIF orientation tag ID as fallback


def preserve_exif_orientation(img, resized_img):
    """
    Preserve EXIF orientation data in the resized image.
    """
    try:
        # Check if image has EXIF data
        if hasattr(img, '_getexif') and img._getexif() is not None:
            exif = img._getexif()
            orientation_tag_id = get_orientation_tag_id()
            
            # Create new EXIF dict for resized image
            exif_dict = {}
            
            # Copy all EXIF data
            for tag_id, value in exif.items():
                tag = ExifTags.TAGS.get(tag_id, tag_id)
                exif_dict[tag] = value
            
            # Ensure orientation is preserved
            if orientation_tag_id in exif:
                orientation_value = exif[orientation_tag_id]
                print(f"    Preserving EXIF orientation: {orientation_value}")
                
                # Convert EXIF dict back to bytes for saving
                exif_bytes = img.info.get('exif', b'')
                return exif_bytes
            
    except Exception as e:
        print(f"    Warning: Could not preserve EXIF data: {e}")
        
    return None


def process_image(source_path: str, dest_path: str, max_size: int = 1500) -> bool:
    """
    Process a single image: resize if needed or copy if not.
    Preserves EXIF orientation data during resize.
    Returns True if successful, False otherwise.
    """
    try:
        with Image.open(source_path) as img:
            original_width, original_height = img.size
            print(f"Processing: {os.path.basename(source_path)} ({original_width}x{original_height})")
            
            # Check for EXIF orientation
            orientation = 1  # Default orientation
            exif_data = None
            orientation_tag_id = get_orientation_tag_id()
            
            try:
                if hasattr(img, '_getexif') and img._getexif() is not None:
                    exif = img._getexif()
                    if exif and orientation_tag_id in exif:
                        orientation = exif[orientation_tag_id]
                        print(f"    Original EXIF orientation: {orientation}")
                
                # Get original EXIF data as bytes
                exif_data = img.info.get('exif', b'')
                
            except Exception as e:
                print(f"    Warning: Could not read EXIF data: {e}")
            
            new_width, new_height = calculate_new_dimensions(original_width, original_height, max_size)
            
            # Ensure destination directory exists
            ensure_directory_exists(os.path.dirname(dest_path))
            
            if (new_width, new_height) == (original_width, original_height):
                # No resize needed, copy the file to preserve all metadata
                shutil.copy2(source_path, dest_path)
                print(f"  → Copied (no resize needed)")
            else:
                # Resize needed - preserve orientation
                resized_img = img.resize((new_width, new_height), Image.Resampling.LANCZOS)
                
                # Preserve original format and quality with EXIF data
                save_kwargs = {}
                
                if img.format == 'JPEG':
                    save_kwargs = {
                        'format': 'JPEG',
                        'quality': 95,
                        'optimize': True,
                        'exif': exif_data if exif_data else b''
                    }
                elif img.format == 'PNG':
                    save_kwargs = {
                        'format': 'PNG',
                        'optimize': True
                    }
                    # PNG doesn't support EXIF, but we can preserve other metadata
                    if hasattr(img, 'info'):
                        save_kwargs['pnginfo'] = img.info
                else:
                    save_kwargs = {'format': img.format}
                    # Try to preserve EXIF for other formats that support it
                    if exif_data and img.format in ['TIFF', 'WebP']:
                        save_kwargs['exif'] = exif_data
                
                # Save with preserved metadata
                resized_img.save(dest_path, **save_kwargs)
                print(f"  → Resized to {new_width}x{new_height} (orientation preserved)")
            
            return True
            
    except Exception as e:
        print(f"  → Error processing {source_path}: {e}")
        return False


def walk_and_resize_images(source_dir: str, dest_dir: str, max_size: int = 1500) -> None:
    """
    Walk through source directory and subdirectories, process all images.
    """
    if not os.path.exists(source_dir):
        print(f"Error: Source directory {source_dir} does not exist!")
        return
    
    # Ensure destination directory exists
    ensure_directory_exists(dest_dir)
    
    # Supported image extensions
    supported_extensions = {'.jpg', '.jpeg', '.png', '.gif', '.bmp', '.tiff', '.webp'}
    
    processed_count = 0
    copied_count = 0
    resized_count = 0
    failed_count = 0
    
    print(f"Starting image processing...")
    print(f"Source: {source_dir}")
    print(f"Destination: {dest_dir}")
    print(f"Max size: {max_size}px")
    print("Preserving EXIF orientation data...")
    print("-" * 50)
    
    for root, dirs, files in os.walk(source_dir):
        # Calculate relative path from source to maintain directory structure
        rel_path = os.path.relpath(root, source_dir)
        dest_root = os.path.join(dest_dir, rel_path) if rel_path != '.' else dest_dir
        
        print(f"\nProcessing folder: {root}")
        
        for file in files:
            file_path = os.path.join(root, file)
            ext = os.path.splitext(file.lower())[1]
            
            if ext in supported_extensions:
                dest_file_path = os.path.join(dest_root, file)
                
                # Check original dimensions to determine if resize is needed
                try:
                    with Image.open(file_path) as img:
                        width, height = img.size
                        new_width, new_height = calculate_new_dimensions(width, height, max_size)
                        needs_resize = (new_width, new_height) != (width, height)
                except:
                    needs_resize = False
                
                success = process_image(file_path, dest_file_path, max_size)
                
                if success:
                    processed_count += 1
                    if needs_resize:
                        resized_count += 1
                    else:
                        copied_count += 1
                else:
                    failed_count += 1
    
    # Print summary
    print("\n" + "=" * 50)
    print("PROCESSING SUMMARY")
    print("=" * 50)
    print(f"Total images processed: {processed_count}")
    print(f"Images resized: {resized_count}")
    print(f"Images copied (no resize): {copied_count}")
    print(f"Images failed: {failed_count}")
    print(f"Destination: {dest_dir}")
    print("EXIF orientation data preserved where possible.")


def main():
    """
    Main function to process images.
    """
    # Configuration
    source_directory = "/home/pimedia/Pictures/test"  # Change this to your source directory
    destination_directory = "/home/pimedia/Pictures/test2"
    max_dimension = 1500
    
    print("Image Resize and Copy Tool with EXIF Preservation")
    print("=" * 50)
    
    # Prompt user for source directory (optional)
    user_source = input(f"Enter source directory (default: {source_directory}): ").strip()
    if user_source:
        source_directory = user_source
    
    # Start processing
    walk_and_resize_images(source_directory, destination_directory, max_dimension)


if __name__ == "__main__":
    main()

# WTF is going on